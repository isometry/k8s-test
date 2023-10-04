package main

import (
	"context"
	_ "embed"
	"fmt"
	"html/template"
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

func getBasicInfo() (data map[string]string) {
	data = map[string]string{
		"podname": os.Getenv("HOSTNAME"),
		"podtime": time.Now().Format(time.RFC3339),
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
	if os.Getenv("KUBERNETES_SERVICE_HOST") == "" || os.Getenv("KUBERNETES_SERVICE_PORT") == "" || namespace == "" {
		return nil
	}

	appInfo := make(map[string]string)
	config, err := rest.InClusterConfig()
	if err != nil {
		appInfo["error"] = fmt.Errorf("config error: %w", err).Error()
		return appInfo
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		appInfo["error"] = fmt.Errorf("clientset error: %w", err).Error()
		return appInfo
	}
	pod, err := clientset.CoreV1().Pods(namespace).Get(context.TODO(), hostname, metav1.GetOptions{})
	if err != nil {
		appInfo["error"] = fmt.Errorf("pods error: %w", err).Error()
		return appInfo
	}
	if len(pod.Spec.Containers) > 0 {
		appInfo["spec.image"] = pod.Spec.Containers[0].Image
	}
	if len(pod.Status.ContainerStatuses) > 0 {
		appInfo["status.image"] = pod.Status.ContainerStatuses[0].Image
	}
	appInfo["node"] = pod.Spec.NodeName
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
			"title": "go-test-service",
		}
		index.Execute(w, data)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	http.ListenAndServe(fmt.Sprintf(":%s", port), nil)
}
