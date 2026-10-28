package gateway

import (
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/user-no-found/FnPackKit/runtime/runtimepanel"
)

func TestPhonePanelAssetsAgainstSharedInputs(t *testing.T) {
	panel := &runtimepanel.Handler{Prefix: "/app/tuios"}
	assets, err := newPanelAssets(panel)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"panel.js", "presentation.js", "drag.js", "panel.css", "fntuios-idle.js"} {
		url := panel.Prefix + "/_runtime/" + name
		get := httptest.NewRecorder()
		if !assets.serve(get, httptest.NewRequest("GET", url, nil)) || get.Body.Len() == 0 {
			t.Fatal("missing adapted resource", name)
		}
		head := httptest.NewRecorder()
		if !assets.serve(head, httptest.NewRequest("HEAD", url, nil)) || head.Body.Len() != 0 || head.Header().Get("Content-Length") != strconv.Itoa(get.Body.Len()) {
			t.Fatal("HEAD differs from GET", name)
		}
		if assets.serve(httptest.NewRecorder(), httptest.NewRequest("POST", url, nil)) {
			t.Fatal("static resource accepted mutation", name)
		}
		if _, err := adaptPanelResource(name, "changed shared input"); name != "fntuios-idle.js" && err == nil {
			t.Fatal("source hook drift silently accepted", name)
		}
	}
}
