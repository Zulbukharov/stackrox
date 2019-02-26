package clusterentities

import (
	"sync"

	"github.com/stackrox/rox/pkg/net"
	"github.com/stackrox/rox/pkg/networkentity"
	"github.com/stackrox/rox/pkg/set"
)

// ContainerMetadata is the container metadata that is stored per instance
type ContainerMetadata struct {
	DeploymentID  string
	PodID         string
	ContainerName string
	ContainerID   string
}

// Store is a store for managing cluster entities (currently deployments only) and allows looking them up by
// endpoint.
type Store struct {
	// ipMap maps ip addresses to sets of deployment ids this IP is associated with.
	ipMap map[net.IPAddress]map[string]struct{}
	// endpointMap maps endpoints to a (deployment id -> endpoint target info) mapping.
	endpointMap map[net.NumericEndpoint]map[string]map[EndpointTargetInfo]struct{}
	// containerIDMap maps container IDs to container metadata
	containerIDMap map[string]ContainerMetadata

	// reverseIpMap maps deployment ids to sets of IP addresses associated with this deployment.
	reverseIPMap map[string]map[net.IPAddress]struct{}
	// reverseEndpointMap maps deployment ids to sets of endpoints associated with this deployment.
	reverseEndpointMap map[string]map[net.NumericEndpoint]struct{}
	// reverseContainerIDMap maps deployment ids to sets of container IDs associated with this deployment.
	reverseContainerIDMap map[string]map[string]struct{}
	// callbackContainerIDSet is a set of container ID's to be resolved
	callbackContainerIDSet set.StringSet
	// callbackChannel is a channel to send container metadata upon resolution
	callbackChannel chan<- ContainerMetadata

	mutex sync.RWMutex
}

// NewStore creates and returns a new store instance.
// Note: Generally, you probably do not want to call this function, but use the singleton instance returned by
// `StoreInstance()`.
func NewStore() *Store {
	return &Store{
		ipMap:                  make(map[net.IPAddress]map[string]struct{}),
		endpointMap:            make(map[net.NumericEndpoint]map[string]map[EndpointTargetInfo]struct{}),
		containerIDMap:         make(map[string]ContainerMetadata),
		reverseIPMap:           make(map[string]map[net.IPAddress]struct{}),
		reverseEndpointMap:     make(map[string]map[net.NumericEndpoint]struct{}),
		reverseContainerIDMap:  make(map[string]map[string]struct{}),
		callbackContainerIDSet: set.NewStringSet(),
	}
}

// EndpointTargetInfo is the target port for an endpoint (container port, service port etc.).
type EndpointTargetInfo struct {
	ContainerPort uint16
	PortName      string
}

// EntityData is a data structure representing the updates to be applied to the store for a given deployment.
type EntityData struct {
	ips          map[net.IPAddress]struct{}
	endpoints    map[net.NumericEndpoint][]EndpointTargetInfo
	containerIDs map[string]ContainerMetadata
}

// AddIP adds an IP address to the set of IP addresses of the respective deployment.
func (ed *EntityData) AddIP(ip net.IPAddress) {
	if ed.ips == nil {
		ed.ips = make(map[net.IPAddress]struct{})
	}
	ed.ips[ip] = struct{}{}
}

// AddEndpoint adds an endpoint along with a target info to the endpoints of the respective deployment.
func (ed *EntityData) AddEndpoint(ep net.NumericEndpoint, info EndpointTargetInfo) {
	if ed.endpoints == nil {
		ed.endpoints = make(map[net.NumericEndpoint][]EndpointTargetInfo)
	}
	ed.endpoints[ep] = append(ed.endpoints[ep], info)
}

// AddContainerID adds a container ID to the container IDs of the respective deployment.
func (ed *EntityData) AddContainerID(containerID string, container ContainerMetadata) {
	if ed.containerIDs == nil {
		ed.containerIDs = make(map[string]ContainerMetadata)
	}
	ed.containerIDs[containerID] = container
}

// Apply applies an update to the store. If incremental is true, data will be added; otherwise, data for each deployment
// that is a key in the map will be replaced (or deleted).
func (e *Store) Apply(updates map[string]*EntityData, incremental bool) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.applyNoLock(updates, incremental)
}

func (e *Store) purgeNoLock(deploymentID string) {
	for ip := range e.reverseIPMap[deploymentID] {
		set := e.ipMap[ip]
		delete(set, deploymentID)
		if len(set) == 0 {
			delete(e.ipMap, ip)
		}
	}
	for ep := range e.reverseEndpointMap[deploymentID] {
		set := e.endpointMap[ep]
		delete(set, deploymentID)
		if len(set) == 0 {
			delete(e.endpointMap, ep)
		}
	}
	for containerID := range e.reverseContainerIDMap[deploymentID] {
		delete(e.containerIDMap, containerID)
	}

	delete(e.reverseIPMap, deploymentID)
	delete(e.reverseEndpointMap, deploymentID)
	delete(e.reverseContainerIDMap, deploymentID)
}

func (e *Store) applyNoLock(updates map[string]*EntityData, incremental bool) {
	if !incremental {
		for deploymentID := range updates {
			e.purgeNoLock(deploymentID)
		}
	}

	for deploymentID, data := range updates {
		if data == nil {
			continue
		}
		e.applySingleNoLock(deploymentID, *data)
	}
}

func (e *Store) applySingleNoLock(deploymentID string, data EntityData) {
	reverseEPs := e.reverseEndpointMap[deploymentID]
	reverseIPs := e.reverseIPMap[deploymentID]
	reverseContainerIDs := e.reverseContainerIDMap[deploymentID]

	for ep, targetInfos := range data.endpoints {
		if reverseEPs == nil {
			reverseEPs = make(map[net.NumericEndpoint]struct{})
			e.reverseEndpointMap[deploymentID] = reverseEPs
		}
		reverseEPs[ep] = struct{}{}

		epMap := e.endpointMap[ep]
		if epMap == nil {
			epMap = make(map[string]map[EndpointTargetInfo]struct{})
			e.endpointMap[ep] = epMap
		}
		targetSet := epMap[deploymentID]
		if targetSet == nil {
			targetSet = make(map[EndpointTargetInfo]struct{})
			epMap[deploymentID] = targetSet
		}
		for _, tgtInfo := range targetInfos {
			targetSet[tgtInfo] = struct{}{}
		}
	}

	for ip := range data.ips {
		if reverseIPs == nil {
			reverseIPs = make(map[net.IPAddress]struct{})
			e.reverseIPMap[deploymentID] = reverseIPs
		}
		reverseIPs[ip] = struct{}{}

		ipMap := e.ipMap[ip]
		if ipMap == nil {
			ipMap = make(map[string]struct{})
			e.ipMap[ip] = ipMap
		}
		ipMap[deploymentID] = struct{}{}
	}

	var mdsForCallback []ContainerMetadata

	for containerID, metadata := range data.containerIDs {
		if reverseContainerIDs == nil {
			reverseContainerIDs = make(map[string]struct{})
			e.reverseContainerIDMap[deploymentID] = reverseContainerIDs
		}
		reverseContainerIDs[containerID] = struct{}{}
		e.containerIDMap[containerID] = metadata
		if found := e.callbackContainerIDSet.Contains(containerID); found {
			mdsForCallback = append(mdsForCallback, metadata)
			e.callbackContainerIDSet.Remove(containerID)
		}
	}

	if e.callbackChannel != nil && len(mdsForCallback) > 0 {
		go sendMetadataCallbacks(e.callbackChannel, mdsForCallback)
	}
}

func sendMetadataCallbacks(callbackC chan<- ContainerMetadata, mds []ContainerMetadata) {
	for _, md := range mds {
		callbackC <- md
	}
}

// RegisterContainerMetadataCallbackChannel registers the given channel as the callback channel for container metadata.
// Any previously registered callback channel will get overwritten by repeatedly calling this method. The previous
// callback channel (if any) is returned by this function.
func (e *Store) RegisterContainerMetadataCallbackChannel(callbackChan chan<- ContainerMetadata) chan<- ContainerMetadata {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	oldChan := e.callbackChannel
	e.callbackChannel = callbackChan
	return oldChan
}

// AddCallbackForContainerMetadata adds a channel to send the container metadata when available.
func (e *Store) AddCallbackForContainerMetadata(containerID string) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	e.callbackContainerIDSet.Add(containerID)
}

// LookupResult contains the result of a lookup operation.
type LookupResult struct {
	Entity         networkentity.Entity
	ContainerPorts []uint16
	PortNames      []string
}

// LookupByEndpoint returns possible target deployments by endpoint (if any).
func (e *Store) LookupByEndpoint(endpoint net.NumericEndpoint) []LookupResult {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	return e.lookupNoLock(endpoint)
}

// LookupByContainerID retrieves the deployment ID by a container ID.
func (e *Store) LookupByContainerID(containerID string) (ContainerMetadata, bool) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	metadata, ok := e.containerIDMap[containerID]
	return metadata, ok
}

func (e *Store) lookupNoLock(endpoint net.NumericEndpoint) (results []LookupResult) {
	for deploymentID, targetInfoSet := range e.endpointMap[endpoint] {
		result := LookupResult{
			Entity:         networkentity.ForDeployment(deploymentID),
			ContainerPorts: make([]uint16, 0, len(targetInfoSet)),
		}
		for tgtInfo := range targetInfoSet {
			result.ContainerPorts = append(result.ContainerPorts, tgtInfo.ContainerPort)
			if tgtInfo.PortName != "" {
				result.PortNames = append(result.PortNames, tgtInfo.PortName)
			}
		}
		results = append(results, result)
	}

	if len(results) > 0 {
		return
	}

	for deploymentID := range e.ipMap[endpoint.IPAndPort.Address] {
		result := LookupResult{
			Entity:         networkentity.ForDeployment(deploymentID),
			ContainerPorts: []uint16{endpoint.IPAndPort.Port},
		}
		results = append(results, result)
	}

	return
}
