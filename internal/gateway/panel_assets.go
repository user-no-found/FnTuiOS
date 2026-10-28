package gateway

import (
	"bytes"
	"embed"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/user-no-found/FnPackKit/runtime/identity"
	"github.com/user-no-found/FnPackKit/runtime/runtimepanel"
)

//go:embed panel-ui/*
var panelUI embed.FS

type panelResource struct {
	data []byte
	kind string
}
type panelAssets map[string]panelResource
type panelResourceWriter struct {
	bytes.Buffer
	header http.Header
	status int
}

func (w *panelResourceWriter) Header() http.Header  { return w.header }
func (w *panelResourceWriter) WriteHeader(code int) { w.status = code }

// newPanelAssets scopes presentation changes to FnTuiOS, leaving the linked toolkit untouched.
func newPanelAssets(panel *runtimepanel.Handler) (panelAssets, error) {
	if panel == nil {
		return nil, nil
	}
	files := make(panelAssets)
	for _, name := range []string{"panel.js", "presentation.js", "drag.js", "panel.css"} {
		request, _ := http.NewRequest(http.MethodGet, "http://runtime.invalid"+panel.Prefix+"/_runtime/"+name, nil)
		response := &panelResourceWriter{header: make(http.Header), status: http.StatusOK}
		if !panel.Handle(response, request, identity.Request{}) || response.status != http.StatusOK {
			return nil, fmt.Errorf("FnTuiOS runtime panel resource unavailable: %s", name)
		}
		data, err := adaptPanelResource(name, response.String())
		if err != nil {
			return nil, err
		}
		files[panel.Prefix+"/_runtime/"+name] = panelResource{[]byte(data), response.header.Get("Content-Type")}
	}
	idle, err := panelUI.ReadFile("panel-ui/idle.js")
	if err != nil {
		return nil, err
	}
	files[panel.Prefix+"/_runtime/fntuios-idle.js"] = panelResource{idle, "text/javascript; charset=utf-8"}
	return files, nil
}

func adaptPanelResource(name, source string) (string, error) {
	var hooks [][2]string
	switch name {
	case "panel.js":
		hooks = [][2]string{
			{"if(!panel.hidden)close.focus({preventScroll:true});", "if(!panel.hidden && !window.FnMobileKeyboard?.isOpen())close.focus({preventScroll:true});"},
		}
	case "presentation.js":
		source = "import { installIdle } from './fntuios-idle.js';\n" + source
		hooks = [][2]string{
			{"    focusLauncher() {", "    focusLauncher() {\n      if (window.FnMobileKeyboard?.isOpen()) return;"},
			{"() => available, () => !viewport.compact);", "() => available, () => true);\n  installIdle(button, panel, drag, close);"},
			{"    if (viewport.compact) {", "    drag.place();\n    if (viewport.compact) {"},
			{"      button.style.left = (available.left + 8)+'px';\n      button.style.top = Math.max(available.top + 8, bottom - 52)+'px';\n", ""},
			{"    } else drag.place();", "    }"},
		}
	case "drag.js":
		hooks = [][2]string{
			{"    if (e.button !== 0 || !enabled()) return;", "    if (e.button !== 0 || !enabled()) return;\n    suppressClick = false;"},
			{"const rect = button.getBoundingClientRect();", "const rect = { left: parseFloat(button.style.left), top: parseFloat(button.style.top) };"},
			{"button.addEventListener('pointercancel',release);", "button.addEventListener('pointercancel',release);\n  button.addEventListener('lostpointercapture',release);"},
			{"return { place };", `return { place, dock() {
    const max = bounds();
    const edges = [['left',position.x*max.x],['right',(1-position.x)*max.x],['top',position.y*max.y],['bottom',(1-position.y)*max.y]];
    position.edge = edges.sort((a,b)=>a[1]-b[1])[0][0];
    save(); place();
  } };`},
		}
	case "panel.css":
		if strings.Count(source, `:host([data-compact="true"]) .toggle {`) != 1 {
			return "", fmt.Errorf("FnTuiOS runtime panel source hook changed: %s", name)
		}
		css, err := panelUI.ReadFile("panel-ui/controls.css")
		if err != nil {
			return "", err
		}
		source += "\n" + string(css)
	}
	for _, hook := range hooks {
		if strings.Count(source, hook[0]) != 1 {
			return "", fmt.Errorf("FnTuiOS runtime panel source hook changed: %s", name)
		}
		source = strings.Replace(source, hook[0], hook[1], 1)
	}
	return source, nil
}

func (assets panelAssets) serve(w http.ResponseWriter, r *http.Request) bool {
	resource, ok := assets[r.URL.Path]
	if !ok || r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	w.Header().Set("Content-Type", resource.kind)
	w.Header().Set("Content-Length", strconv.Itoa(len(resource.data)))
	if r.Method != http.MethodHead {
		_, _ = w.Write(resource.data)
	}
	return true
}
