package clusters

import (
	"text/template"

	"bitbucket.org/stack-rox/apollo/generated/api/v1"
	"bitbucket.org/stack-rox/apollo/pkg/env"
	kubernetesPkg "bitbucket.org/stack-rox/apollo/pkg/kubernetes"
	"bitbucket.org/stack-rox/apollo/pkg/zip"
)

func init() {
	deployers[v1.ClusterType_KUBERNETES_CLUSTER] = newKubernetes()
}

type kubernetes struct {
	deploy *template.Template
	cmd    *template.Template
	rbac   *template.Template
	delete *template.Template
}

func newKubernetes() Deployer {
	return &kubernetes{
		deploy: template.Must(template.New("kubernetes").Parse(k8sDeploy)),
		cmd:    template.Must(template.New("kubernetes").Parse(k8sCmd)),
		rbac:   template.Must(template.New("kubernetes").Parse(k8sRBAC)),
		delete: template.Must(template.New("kubernetes").Parse(k8sDelete)),
	}
}

func nonEmptyOrDefault(new, def string) string {
	if new != "" {
		return new
	}
	return def
}

func addCommonKubernetesParams(params *v1.CommonKubernetesParams, fields map[string]string) {
	fields["Namespace"] = nonEmptyOrDefault(params.GetNamespace(), "stackrox")
}

func (k *kubernetes) Render(c Wrap) ([]*v1.File, error) {
	var kubernetesParams *v1.KubernetesParams
	clusterKube, ok := c.OrchestratorParams.(*v1.Cluster_Kubernetes)
	if ok {
		kubernetesParams = clusterKube.Kubernetes
	}

	fields := fieldsFromWrap(c)
	addCommonKubernetesParams(kubernetesParams.GetParams(), fields)

	fields["OpenshiftAPIEnv"] = env.OpenshiftAPI.EnvVar()
	fields["OpenshiftAPI"] = `"false"`

	fields["ImagePullSecretEnv"] = env.ImagePullSecrets.EnvVar()
	fields["ImagePullSecret"] = nonEmptyOrDefault(kubernetesParams.GetParams().GetImagePullSecret(), "stackrox")

	var err error
	fields["Registry"], err = kubernetesPkg.GetResolvedRegistry(c.PreventImage)
	if err != nil {
		return nil, err
	}

	var files []*v1.File
	data, err := executeTemplate(k.deploy, fields)
	if err != nil {
		return nil, err
	}
	files = append(files, zip.NewFile("deploy.yaml", data, false))

	data, err = executeTemplate(k.cmd, fields)
	if err != nil {
		return nil, err
	}
	files = append(files, zip.NewFile("deploy.sh", data, true))

	data, err = executeTemplate(k.rbac, fields)
	if err != nil {
		return nil, err
	}
	files = append(files, zip.NewFile("rbac.yaml", data, false))
	data, err = executeTemplate(k.delete, fields)
	if err != nil {
		return nil, err
	}
	files = append(files, zip.NewFile("delete.sh", data, true))
	return files, nil
}

var (
	k8sDeploy = `apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: sensor
  namespace: {{.Namespace}}
  labels:
    app: sensor
  annotations:
    owner: stackrox
    email: support@stackrox.com
spec:
  replicas: 1
  selector:
    matchLabels:
      app: sensor
  template:
    metadata:
      namespace: {{.Namespace}}
      labels:
        app: sensor
    spec:
      containers:
      - image: {{.Image}}
        resources:
          requests:
            memory: "200Mi"
            cpu: "200m"
          limits:
            memory: "500Mi"
            cpu: "500m"
        securityContext:
          capabilities:
            drop: ["NET_RAW"]
        env:
        - name: {{.PublicEndpointEnv}}
          value: {{.PublicEndpoint}}
        - name: {{.ClusterIDEnv}}
          value: {{.ClusterID}}
        - name: {{.ImageEnv}}
          value: {{.Image}}
        - name: {{.AdvertisedEndpointEnv}}
          value: sensor.{{.Namespace}}:443
{{if .ImagePullSecret }}
        - name: {{.ImagePullSecretEnv}}
          value: {{.ImagePullSecret}}
{{- end}}
        - name: {{.OpenshiftAPIEnv}}
          value: {{.OpenshiftAPI}}
        - name: ROX_PREVENT_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        - name: ROX_PREVENT_SERVICE_ACCOUNT
          valueFrom:
            fieldRef:
              fieldPath: spec.serviceAccountName
        imagePullPolicy: Always
        name: sensor
        command:
        - kubernetes-sensor
        volumeMounts:
        - name: certs
          mountPath: /run/secrets/stackrox.io/
          readOnly: true
        resources:
          requests:
            memory: "200Mi"
            cpu: "200m"
          limits:
            memory: "500Mi"
            cpu: "500m"
      serviceAccount: sensor
{{if .ImagePullSecret }}
      imagePullSecrets:
      - name: {{.ImagePullSecret}}
{{- end}}
      volumes:
      - name: certs
        secret:
          secretName: sensor-tls
          items:
          - key: sensor-cert.pem
            path: cert.pem
          - key: sensor-key.pem
            path: key.pem
          - key: central-ca.pem
            path: ca.pem
---
apiVersion: v1
kind: Service
metadata:
  name: sensor
  namespace: {{.Namespace}}
spec:
  ports:
  - name: https
    port: 443
    targetPort: 443
  selector:
    app: sensor
  type: ClusterIP
`

	k8sRBAC = `
apiVersion: v1
kind: ServiceAccount
metadata:
  name: sensor
  namespace: {{.Namespace}}
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: benchmark
  namespace: {{.Namespace}}
---
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: {{.Namespace}}:monitor-deployments
subjects:
- kind: ServiceAccount
  name: sensor
  namespace: {{.Namespace}}
roleRef:
  kind: ClusterRole
  name: view
  apiGroup: rbac.authorization.k8s.io
---
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: {{.Namespace}}:enforce-policies
subjects:
- kind: ServiceAccount
  name: sensor
  namespace: {{.Namespace}}
roleRef:
  kind: ClusterRole
  name: edit
  apiGroup: rbac.authorization.k8s.io
---
kind: RoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: launch-benchmarks
  namespace: {{.Namespace}}
subjects:
- kind: ServiceAccount
  name: sensor
  namespace: {{.Namespace}}
roleRef:
  kind: ClusterRole
  name: edit
  apiGroup: rbac.authorization.k8s.io
`

	k8sCmd = commandPrefix + kubernetesPkg.GetCreateSecretTemplate("{{.Namespace}}", "{{.Registry}}", "{{.ImagePullSecret}}") + `
kubectl create -f "$DIR/rbac.yaml"
kubectl create secret -n "{{.Namespace}}" generic sensor-tls --from-file="$DIR/sensor-cert.pem" --from-file="$DIR/sensor-key.pem" --from-file="$DIR/central-ca.pem"
kubectl create -f "$DIR/deploy.yaml"
`

	k8sDelete = commandPrefix + `
	kubectl delete -f "$DIR/deploy.yaml"
	kubectl delete -n {{.Namespace}} secret/sensor-tls
`
)
