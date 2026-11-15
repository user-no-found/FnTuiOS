package gateway

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// This fixture uses the real business input/control adapters with an inert client.
// It exercises DOM ownership without starting a terminal or modifying a NAS session.
func TestGatewaySharedPanelBrowser(t *testing.T) {
	if os.Getenv("FNTUIOS_PANEL_BROWSER_TEST") != "1" {
		t.Skip("set FNTUIOS_PANEL_BROWSER_TEST=1 with BuildFlow browser environment")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	business := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, name := range []string{"mobile-input.js", "mobile-keyboard.js", "panel-controls.js"} {
			if r.URL.Path == "/static/"+name {
				data, err := os.ReadFile(filepath.Join(root, "assets", name))
				if err != nil {
					t.Error(err)
					http.Error(w, "fixture asset unavailable", 500)
					return
				}
				w.Header().Set("Content-Type", "text/javascript")
				_, _ = w.Write(data)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(panelBusinessHTML))
	})
	front := httptest.NewUnstartedServer(nil)
	defer front.Close()
	fixture := newPanelFixture(t, business, front.Listener.Addr().String())
	front.Config.Handler = &httputil.ReverseProxy{Transport: fixture.transport, Rewrite: func(r *httputil.ProxyRequest) {
		r.Out.URL.Scheme = "http"
		r.Out.URL.Host = r.In.Host
		r.Out.Header.Set("X-Trim-Userid", "1000")
		r.Out.Header.Set("X-Trim-Username", "panel-user")
	}}
	front.Start()
	output := os.Getenv("FNTUIOS_PANEL_BROWSER_OUTPUT")
	if output == "" {
		output = t.TempDir()
	}
	node := os.Getenv("BUILDFLOW_BROWSER_NODE")
	if node == "" || os.Getenv("BUILDFLOW_BROWSER_RUNTIME") == "" {
		t.Fatal("BuildFlow resolve_browser environment required")
	}
	command := exec.CommandContext(t.Context(), node, filepath.Join(root, "scripts/verify_shared_panel.mjs"), "--url", front.URL+"/app/tuios/", "--output", output)
	command.Env = os.Environ()
	data, err := command.CombinedOutput()
	t.Log(string(data))
	if err != nil {
		t.Fatal(err)
	}
}

const panelBusinessHTML = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><link rel="icon" href="data:,">
<style>body{margin:0;background:#eef2f6}textarea{position:absolute;top:24px;left:24px;width:240px;height:90px}#sip-keybar{position:fixed;bottom:0;height:72px;width:100%;background:#cbd5e1}</style>
</head><body><div id="terminal"><textarea inputmode="text" aria-label="终端输入"></textarea></div><div id="sip-keybar">terminal toolbar</div><button id="settings-toggle">settings target</button>
<script src="static/mobile-input.js"></script><script src="static/mobile-keyboard.js"></script><script src="static/panel-controls.js"></script>
<script>
window.__calls=[];window.__keys=[];
window.WebTerm={encodeKey(event){return event.type==='keydown'?event.key+':'+Number(event.ctrlKey)+Number(event.shiftKey)+Number(event.altKey):'';},mobile:{encodeKeySpec(){return '';}}};
window.FnTextCopy={install(){return {sync(){},receive(){return false;},open(){window.__calls.push('copy-text');}};}};
window.FnImagePaste={choose(){window.__calls.push('photo');}};
const target=document.querySelector('textarea');
for(const type of ['keydown','keyup'])target.addEventListener(type,event=>window.__keys.push({type:event.type,key:event.key}));
const client=window.sipTerm={connected:true,readOnly:false,webterm:{xterm:{textarea:target},keyboard:{flags:0},paste(value){window.__calls.push('paste:'+value);}},
mobile:{enabled:true,rescueUntil:0,setKeyboardOpen(value){window.__keyboardOpen=value;}},
sendInput(value){window.__calls.push('input:'+value);return Promise.resolve();},
connection:{sendMessage(type,data){const request=JSON.parse(new TextDecoder().decode(data));window.__calls.push(request.direction);setTimeout(()=>{const result=new TextEncoder().encode(JSON.stringify({type:'panel-result',id:request.id}));window.FnPanelControls.receive(client,new Uint8Array([type,...result]));},0);return Promise.resolve();}}};
window.FnMobileKeyboard.install(client);window.FnPanelControls.install(client);
document.querySelector('#settings-toggle').onclick=()=>window.__calls.push('terminal-settings');
</script></body></html>`
