package main

import (
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/carlmjohnson/versioninfo"
	"github.com/gosimple/slug"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
)

type SectionData struct {
	Title string
	Data  map[string]string
}

//go:embed index.html.tmpl
var indexTemplate string

//go:embed style.css.tmpl
var styleTemplate string

//go:embed script.js.tmpl
var scriptTemplate string

//go:embed data.html.tmpl
var dataTemplate string

var startTime time.Time

type kubeconfig struct {
	config *rest.Config
	Client *kubernetes.Clientset
}

var kubectx *kubeconfig

func init() {
	if os.Getenv("KUBERNETES_SERVICE_HOST") == "" || os.Getenv("KUBERNETES_SERVICE_PORT") == "" {
		return
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		log.Printf("error loading in-cluster kubernetes config: %v", err)
		return
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Printf("error loading kubernetes clientset : %v", err)
		return
	}

	kubectx = &kubeconfig{
		config: config,
		Client: clientset,
	}
}

func getBasicInfo() (data map[string]string) {
	data = map[string]string{
		"podname": os.Getenv("HOSTNAME"),
		"podtime": time.Now().Format(time.RFC3339),
		"runtime": time.Since(startTime).Truncate(time.Second).String(),
	}
	return
}

func getVersionInfo() (data map[string]string) {
	data = make(map[string]string)

	data["version"] = versioninfo.Version
	data["last-commit"] = versioninfo.LastCommit.Format(time.RFC3339)
	data["revision"] = versioninfo.Revision

	return data
}

func getEnv() (env map[string]string) {
	env = make(map[string]string, 1)

	for _, e := range os.Environ() {
		pair := strings.SplitN(e, "=", 2)
		env[pair[0]] = pair[1]
	}
	return env
}

func getPodInfo(path string) (podinfo map[string]string) {
	var fileName, filePath string
	podinfo = make(map[string]string)
	if path == "" {
		path = "/etc/podinfo"
	}
	files, err := os.ReadDir(path)
	if err != nil {
		return nil
	}
	for _, file := range files {
		fileName = file.Name()
		if strings.HasPrefix(fileName, ".") {
			continue
		}
		filePath = filepath.Join(path, fileName)
		content, err := os.ReadFile(filePath)
		if err != nil {
			podinfo[fileName] = string(err.Error())
		}
		podinfo[fileName] = string(content)
	}
	return podinfo
}

func getApiInfo() map[string]string {
	hostname := os.Getenv("HOSTNAME")
	namespace := os.Getenv("METADATA_NAMESPACE")
	if kubectx == nil || namespace == "" {
		return nil
	}

	appInfo := make(map[string]string)
	pod, err := kubectx.Client.CoreV1().Pods(namespace).Get(context.TODO(), hostname, metav1.GetOptions{})
	if err != nil {
		appInfo["error"] = fmt.Errorf("pods error: %w", err).Error()
		return appInfo
	}
	if len(pod.Spec.Containers) > 0 {
		appInfo["spec.image"] = pod.Spec.Containers[0].Image
	}
	if len(pod.Status.ContainerStatuses) > 0 {
		appInfo["status.image"] = pod.Status.ContainerStatuses[0].Image
		appInfo["status.imageID"] = pod.Status.ContainerStatuses[0].ImageID
		appInfo["restartCount"] = fmt.Sprint(pod.Status.ContainerStatuses[0].RestartCount)
		appInfo["startTime"] = pod.Status.StartTime.Format(time.RFC3339)
	}
	appInfo["node"] = pod.Spec.NodeName
	return appInfo
}

func getConfigMap(args ...string) map[string]string {
	if kubectx == nil || len(args) < 1 || len(args) > 2 {
		return nil
	}

	name := args[0]
	if name == "" {
		return nil
	}

	namespace := os.Getenv("METADATA_NAMESPACE")
	if len(args) == 2 {
		namespace = args[1]
	}
	if namespace == "" {
		return nil
	}

	appInfo := make(map[string]string)
	appInfo[".metadata.name"] = name

	cm, err := kubectx.Client.CoreV1().ConfigMaps(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		appInfo["error"] = fmt.Errorf("configmaps error: %w", err).Error()
		return appInfo
	}

	for k, v := range cm.Data {
		appInfo[k] = v
	}

	return appInfo
}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"toSlug": func(r string) string {
			return slug.Make(r)
		},
	}
}

func main() {
	startTime = time.Now()

	index := template.Must(template.New("index").Parse(indexTemplate))
	style := template.Must(template.New("style").Parse(styleTemplate))
	script := template.Must(template.New("script").Parse(scriptTemplate))
	data := template.Must(template.New("data").Funcs(templateFuncs()).Parse(dataTemplate))

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	http.HandleFunc("/style", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		style.Execute(w, map[string]string{
			"background_color": os.Getenv("BACKGROUND_COLOR"),
			"foreground_color": os.Getenv("FOREGROUND_COLOR"),
		})
	})

	http.HandleFunc("/script", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		script.Execute(w, map[string]string{})
	})

	http.HandleFunc("/data", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		data.Execute(w, map[string]any{
			"background_color": os.Getenv("BACKGROUND_COLOR"),
			"foreground_color": os.Getenv("FOREGROUND_COLOR"),
			"sections": []SectionData{
				{
					Title: "Basic",
					Data:  getBasicInfo(),
				},
				{
					Title: "Pod Info",
					Data:  getPodInfo("/etc/podinfo"),
				},
				{
					Title: "ConfigMap",
					Data:  getConfigMap(os.Getenv("CONFIGMAP_NAME"), os.Getenv("CONFIGMAP_NAMESPACE")),
				},
				{
					Title: "API Info",
					Data:  getApiInfo(),
				},
				{
					Title: "Binary Version",
					Data:  getVersionInfo(),
				},
				{
					Title: "Environment Variables",
					Data:  getEnv(),
				},
			},
		})
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		data := map[string]any{
			"title": "k8s-test",
		}
		index.Execute(w, data)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	http.ListenAndServe(fmt.Sprintf(":%s", port), nil)
}
