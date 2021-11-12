package handler

import (
	"archive/zip"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/stackrox/rox/central/cve/fetcher"
	"github.com/stackrox/rox/central/scannerdefinitions/file"
	"github.com/stackrox/rox/pkg/env"
	"github.com/stackrox/rox/pkg/fileutils"
	"github.com/stackrox/rox/pkg/httputil"
	"github.com/stackrox/rox/pkg/httputil/proxy"
	"github.com/stackrox/rox/pkg/logging"
	"github.com/stackrox/rox/pkg/migrations"
	"github.com/stackrox/rox/pkg/sync"
	"github.com/stackrox/rox/pkg/utils"
	"google.golang.org/grpc/codes"
)

const (
	definitionsBaseDir = "scannerdefinitions"

	// scannerDefsSubZipName represents the offline zip bundle for CVEs for Scanner.
	scannerDefsSubZipName = "scanner-defs.zip"
	// K8sIstioCveZipName represents the zip bundle for k8s/istio CVEs.
	K8sIstioCveZipName = "k8s-istio.zip"

	// offlineScannerDefsName represents the offline/fallback zip bundle for CVEs for Scanner.
	offlineScannerDefsName = scannerDefsSubZipName

	scannerUpdateDomain    = "https://definitions.stackrox.io"
	scannerUpdateURLSuffix = "diff.zip"

	defaultCleanupInterval = 4 * time.Hour
	defaultCleanupAge      = 1 * time.Hour
)

var (
	client = &http.Client{
		Transport: proxy.RoundTripper(),
		Timeout:   5 * time.Minute,
	}

	log = logging.LoggerForModule()
)

type requestedUpdater struct {
	*updater
	lastRequestedTime time.Time
}

// httpHandler handles HTTP GET and POST requests for vulnerability data.
type httpHandler struct {
	cveManager fetcher.OrchestratorIstioCVEManager

	online        bool
	interval      time.Duration
	lock          sync.Mutex
	updaters      map[string]*requestedUpdater
	onlineVulnDir string
	offlineFile   *file.Metadata
}

// New creates a new http.Handler to handle vulnerability data.
func New(cveManager fetcher.OrchestratorIstioCVEManager, opts handlerOpts) http.Handler {
	h := &httpHandler{
		cveManager: cveManager,

		online:   !env.OfflineModeEnv.BooleanSetting(),
		interval: env.ScannerVulnUpdateInterval.DurationSetting(),
	}

	h.initializeOfflineVulnDump(opts.offlineVulnDefsDir)

	if h.online {
		h.initializeUpdaters(opts.cleanupInterval, opts.cleanupAge)
	} else {
		log.Info("In offline mode: scanner definitions will not be updated automatically")
	}

	return h
}

func (h *httpHandler) initializeOfflineVulnDump(vulnDefsDir string) {
	if vulnDefsDir == "" {
		vulnDefsDir = filepath.Join(migrations.DBMountPath(), definitionsBaseDir)
	}
	offlinePath := filepath.Join(vulnDefsDir, offlineScannerDefsName)

	// Check if the offline file already exists and set the modified time.
	var lastModifiedTime *time.Time
	f, err := os.Stat(offlinePath)
	// If there is an error reading the file, treat it like the file does not exist.
	// If it does exist, read its modified time.
	if err == nil {
		log.Info("Found uploaded scanner definitions file")
		t := f.ModTime()
		lastModifiedTime = &t
	}

	h.offlineFile = file.NewMetadata(offlinePath, lastModifiedTime)
}

func (h *httpHandler) initializeUpdaters(cleanupInterval, cleanupAge *time.Duration) {
	var err error
	h.onlineVulnDir, err = os.MkdirTemp("", definitionsBaseDir)
	utils.CrashOnError(err) // Fundamental problem if we cannot create a temp directory.

	h.updaters = make(map[string]*requestedUpdater)
	go h.runCleanupUpdaters(cleanupInterval, cleanupAge)
}

func (h *httpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.get(w, r)
	case http.MethodPost:
		h.post(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *httpHandler) get(w http.ResponseWriter, r *http.Request) {
	uuid := r.URL.Query().Get(`uuid`)
	if !h.online || uuid == "" {
		// Default to the offline dump.
		h.offlineFile.RLock()
		defer h.offlineFile.RUnlock()

		h.serveFileNoLock(w, r, h.offlineFile.GetPath())
		return
	}

	u := h.getUpdater(uuid)
	// Start may be called multiple times, but will only start the updater once.
	u.Start()

	// Serve the more recent of the requested file and the manually uploaded definitions.

	serveFile := u.file
	unusedFile := h.offlineFile

	serveFile.RLock()
	unusedFile.RLock()
	if unusedFile.GetLastModifiedTime().After(serveFile.GetLastModifiedTime()) {
		serveFile, unusedFile = unusedFile, serveFile
	}
	unusedFile.RUnlock()
	defer serveFile.RUnlock()

	h.serveFileNoLock(w, r, serveFile.GetPath())
}

func (h *httpHandler) serveFileNoLock(w http.ResponseWriter, r *http.Request, path string) {
	basePath := filepath.Base(path)

	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("No scanner definitions found"))
			return
		}
		httputil.WriteGRPCStyleErrorf(w, codes.Internal, "couldn't stat file %s: %v", basePath, err)
		return
	}

	log.Infof("Serving vulnerability definitions from %s", basePath)
	http.ServeFile(w, r, path)
}

// getUpdater gets or creates the updater for the scanner definitions
// identified by the given uuid.
// If the updater is created here, it is no started here, as it is a blocking operation.
func (h *httpHandler) getUpdater(uuid string) *requestedUpdater {
	h.lock.Lock()
	defer h.lock.Unlock()

	u, exists := h.updaters[uuid]
	if !exists {
		filePath := filepath.Join(h.onlineVulnDir, uuid+".zip")

		h.updaters[uuid] = &requestedUpdater{
			updater: newUpdater(
				file.NewMetadata(filePath, nil),
				client,
				strings.Join([]string{scannerUpdateDomain, uuid, scannerUpdateURLSuffix}, "/"),
				h.interval,
			),
		}

		u = h.updaters[uuid]
	}

	u.lastRequestedTime = time.Now()

	return u
}

func (h *httpHandler) updateK8sIstioCVEs(zipPath string) {
	if !h.online {
		h.cveManager.Update(zipPath, false)
	}
}

func (h *httpHandler) handleScannerDefsFile(zipF *zip.File) error {
	r, err := zipF.Open()
	if err != nil {
		return errors.Wrap(err, "opening ZIP reader")
	}
	defer utils.IgnoreError(r.Close)

	// POST requests only update the offline feed.
	if err := file.Write(h.offlineFile, r, zipF.Modified); err != nil {
		return errors.Wrap(err, "writing scanner definitions")
	}

	return nil
}

func (h *httpHandler) handleZipContentsFromVulnDump(zipPath string) error {
	zipR, err := zip.OpenReader(zipPath)
	if err != nil {
		return errors.Wrap(err, "couldn't open file as zip")
	}
	defer utils.IgnoreError(zipR.Close)

	var scannerDefsFileFound bool
	for _, zipF := range zipR.File {
		if zipF.Name == scannerDefsSubZipName {
			if err := h.handleScannerDefsFile(zipF); err != nil {
				return errors.Wrap(err, "couldn't handle scanner-defs sub file")
			}
			scannerDefsFileFound = true
			continue
		} else if zipF.Name == K8sIstioCveZipName {
			h.updateK8sIstioCVEs(zipPath)
		}
	}

	if !scannerDefsFileFound {
		return errors.New("scanner defs file not found in upload zip; wrong zip uploaded?")
	}
	return nil
}

func (h *httpHandler) post(w http.ResponseWriter, r *http.Request) {
	tempDir, err := os.MkdirTemp("", "scanner-definitions-handler")
	if err != nil {
		httputil.WriteGRPCStyleErrorf(w, codes.Internal, "failed to create temp dir: %v", err)
		return
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			log.Warnf("Failed to remove temp dir for scanner defs: %v", err)
		}
	}()

	tempFile := filepath.Join(tempDir, "tempfile.zip")
	if err := fileutils.CopySrcToFile(tempFile, r.Body); err != nil {
		httputil.WriteGRPCStyleError(w, codes.Internal, errors.Wrapf(err, "copying HTTP POST body to %s", tempFile))
		return
	}

	if err := h.handleZipContentsFromVulnDump(tempFile); err != nil {
		httputil.WriteGRPCStyleError(w, codes.Internal, err)
		return
	}

	_, _ = w.Write([]byte("Successfully stored the offline vulnerability definitions"))
}

func (h *httpHandler) runCleanupUpdaters(cleanupInterval, cleanupAge *time.Duration) {
	interval := defaultCleanupInterval
	if cleanupInterval != nil {
		interval = *cleanupInterval
	}
	age := defaultCleanupAge
	if cleanupAge != nil {
		age = *cleanupAge
	}

	t := time.NewTicker(interval)
	for range t.C {
		h.cleanupUpdaters(age)
	}
}

func (h *httpHandler) cleanupUpdaters(cleanupAge time.Duration) {
	now := time.Now()

	h.lock.Lock()
	defer h.lock.Unlock()

	for id, updatingHandler := range h.updaters {
		if now.Sub(updatingHandler.lastRequestedTime) > cleanupAge {
			// Updater has not been requested for a long time.
			// Clean it up.
			updatingHandler.Stop()
			delete(h.updaters, id)
		}
	}
}
