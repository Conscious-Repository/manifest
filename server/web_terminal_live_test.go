package server

import (
	"os/exec"
	"strings"
	"testing"
)

func TestTerminalFrontendLiveSurfaceOnly(t *testing.T) {
	src := readWebJS(t, "web/js/73-terminal.js")
	for _, removed := range []string{"renderTermHistory", "termHist", "termRename", "termToggleRecent", "resumePicker", "autoName", "/api/terminal/sessions", "setInterval("} {
		if strings.Contains(src, removed) {
			t.Errorf("Terminal retains retired history/state machinery: %s", removed)
		}
	}
	for _, required := range []string{"/api/terminal/live", "termAttachQuery(session)", "manifest-terminal-state", "showFilesStage()", "showActivityStage()", "body.keep = true", `"term-primary", "open"`} {
		if !strings.Contains(src, required) {
			t.Errorf("Terminal missing %s", required)
		}
	}
	html := readWebJS(t, "web/index.html")
	start, end := strings.Index(html, `id="terminalView"`), strings.Index(html, `id="chatView"`)
	if start < 0 || end < start {
		t.Fatal("missing terminal section")
	}
	section := html[start:end]
	for _, removed := range []string{"termHistoryRows", "termHistLabel", "History"} {
		if strings.Contains(section, removed) {
			t.Errorf("retired terminal history DOM remains: %s", removed)
		}
	}
	for _, id := range []string{"termLauncher", "termSessionRows", "termStageFiles", "termStageActivity"} {
		if !strings.Contains(section, id) {
			t.Errorf("lost terminal surface %s", id)
		}
	}
	css := readWebJS(t, "web/css/73-terminal.css")
	if strings.Contains(css, "term-hist") || strings.Contains(css, "term-recent") {
		t.Fatal("dead history styles remain")
	}
}
func TestTerminalFrontendExactLiveTargetsAndRemoteKeep(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable; live Terminal behavior fixture needs JavaScript runtime")
	}
	src := readWebJS(t, "web/js/73-terminal.js")
	script := `
const assert=require('node:assert/strict');
global.cmdRegistry={register(){}};
global.document={hidden:false,getElementById:()=>null,querySelector:()=>null};
global.window={addEventListener(){}};
global.els={terminalView:{hidden:false}};
global.showToast=message=>{throw new Error(message);};
` + src + `
(async()=>{
 const stable={id:'abcdef12',backend:'herdr',live:true,runtime:{pane:'p1',occupant:'t1',generation:'g1',agentSession:'c1'}};
 const orphan={id:'live:t2',backend:'herdr',live:true,handle:'herdr:exact+opaque/value',runtime:{pane:'p2',occupant:'t2',generation:'g1'}};
 assert.equal(termAttachQuery(stable),'id=abcdef12');
 assert.equal(termAttachQuery(orphan),'handle='+encodeURIComponent(orphan.handle));
 assert.notEqual(termRuntimeKey(stable),termRuntimeKey({...stable,runtime:{...stable.runtime,occupant:'replacement'}}));
 assert.notEqual(termRuntimeKey(stable),termRuntimeKey({...stable,runtime:{...stable.runtime,agentSession:'replacement'}}));
 let attached=[],detached=0,requests=[],inventory=[stable,orphan,{id:'dead',live:false}],createdBody;
 renderTermSessions=()=>{};renderTermLauncher=()=>{};renderTermEmpty=()=>{};
 attachTerm=id=>attached.push(id);detachTerm=()=>{detached++;termInst=null;};termSetStage=stage=>{termStage=stage;};
 global.fetch=async(url,opts)=>{requests.push([url,opts]);return {ok:true,json:async()=>({enabled:true,connectivity:'connected',sessions:inventory})};};
 await loadTermSessions();assert.deepEqual(attached,[],'opening Terminal must not auto-select history');assert.equal(termSessions.length,2);
 termOpenId='dead';await loadTermSessions();assert.deepEqual(attached,[],'dead selected row must not be attached/relaunched');
 termOpenId=stable.id;await loadTermSessions();assert.deepEqual(attached,[stable.id]);
 await termKill(orphan);
 const orphanClose=requests.find(([url])=>url==='/api/terminal/live/close');
 assert.equal(orphanClose[1].method,'POST');assert.equal(JSON.parse(orphanClose[1].body).handle,orphan.handle);
 await termKill(stable);assert.ok(requests.some(([url])=>url==='/api/terminal/session/abcdef12/kill'));
 assert.ok(requests.every(([,opts])=>!opts||opts.method!=='DELETE'),'Terminal must not forget conversation history');
 // API outage while the attach client lives is unknown, not process death.
 const beforeOutage=detached;termInst={id:stable.id};termOpenId=stable.id;
 global.fetch=async()=>{throw new Error('socket outage');};
 await loadTermSessions(true);assert.equal(detached,beforeOutage);assert.equal(termConnectivity,'unavailable');
 termDevices=[{name:'remote',self:false,status:'ok',caffeinate:true}];termLaunch.device='remote';termLaunch.cwd='/exact/cwd';termKeepPref=true;
 global.postJSONOk=async(url,body)=>{assert.equal(url,'/api/terminal/session');createdBody=body;return {id:'remote_new'};};
 global.fetch=async()=>({ok:true,json:async()=>({enabled:true,connectivity:'connected',sessions:[{id:'remote_new',live:true}]})});
 await termCreate('shell');assert.equal(createdBody.keep,true);assert.equal(createdBody.cwd,'/exact/cwd');assert.equal(createdBody.device,'remote');assert.ok(!('resumePicker' in createdBody));assert.ok(!('name' in createdBody));
})().catch(err=>{console.error(err);process.exitCode=1;});
`
	if out, err := exec.Command(node, "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("live terminal browser fixture: %v\n%s", err, out)
	}
}

func TestTerminalAttachmentRecovery(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/terminal-recovery.cjs").CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
