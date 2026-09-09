package server

import (
	"os/exec"
	"testing"
)

func TestChatPollIgnoresPreviousConversation(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	script := `
const assert=require('node:assert/strict');
global.cmdRegistry={register(){}};
global.window={addEventListener(){}};
global.document={addEventListener(){},getElementById(){return null;},querySelector(){return null;}};
global.els={chatView:{hidden:false}};
` + readWebJS(t, "web/js/48-chat.js") + `
(async()=>{
 let tick, resolve, paints=0;
 global.setInterval=fn=>{tick=fn;return 1;}; global.clearInterval=()=>{};
 global.fetch=()=>new Promise(r=>{resolve=r;});
 renderChatTranscript=()=>{paints++;};
 chatAgent='alfred'; chatOpenId='old';
 ensureChatPoll({status:'thinking'},0);
 const pending=tick();
 chatOpenId='new'; chatRouteVersion++;
 resolve({ok:true,json:async()=>({session:{updated:'now',status:'idle'},queued:[]})});
 await pending;
 assert.equal(paints,0,'a previous conversation must not replace the current transcript');
})().catch(e=>{console.error(e);process.exit(1);});`
	if out, err := exec.Command(node, "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestTerminalLaunchSuppressesDoubleClick(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	script := `
const assert=require('node:assert/strict');
global.cmdRegistry={register(){}};
global.window={addEventListener(){}};
global.document={getElementById(){return null;},querySelector(){return null;}};
` + readWebJS(t, "web/js/73-terminal.js") + `
(async()=>{
 let calls=0, resolve;
 global.postJSONOk=()=>{calls++;return new Promise(r=>resolve=r);};
 termSetStage=()=>{};loadTermSessions=async()=>{};
 const first=termCreate('shell');
 await termCreate('shell');
 assert.equal(calls,1);
 resolve({id:'one'});await first;
 assert.equal(termCreating,false);
})().catch(e=>{console.error(e);process.exit(1);});`
	if out, err := exec.Command(node, "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestChatInboxOrdersAndFiltersAcrossAgents(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	script := `
const assert=require('node:assert/strict');
global.cmdRegistry={register(){}};
global.window={addEventListener(){}};
global.document={addEventListener(){},getElementById(){return null;},querySelector(){return null;}};
` + readWebJS(t, "web/js/48-chat.js") + `
chatRoster=[{name:'alfred',label:'Alfred'},{name:'zeck',label:'Zeck'}];
chatSessions=[{id:'spirit',title:'Research',created:'2026-09-01'}];
chatAgentSessions={alfred:[{id:'same',title:'Plan',updated:'2026-09-08'}],zeck:[{id:'same',title:'Property review',updated:'2026-09-09'}]};
chatTermEnabled=false;
assert.deepEqual(chatInboxEntries().map(e=>e.agent),['zeck','alfred','']);
chatSearchQuery='property';assert.deepEqual(chatInboxEntries().map(e=>e.agent),['zeck']);
chatSearchQuery='';chatInboxFilter='alfred';assert.equal(chatInboxEntries().length,1);
assert.equal(chatInboxEntries()[0].session.title,'Plan');
chatInboxFilter='all';chatSearchQuery='missing';assert.equal(chatInboxEntries().length,0);
`
	if out, err := exec.Command(node, "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestChatDraftsStayWithTheirConversation(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	script := `
const assert=require('node:assert/strict');
global.cmdRegistry={register(){}};global.window={addEventListener(){}};
let input={value:'Draft for first session'};
global.document={addEventListener(){},querySelector(){return input;},getElementById(){return null;}};
` + readWebJS(t, "web/js/48-chat.js") + `
chatDraftKey='codex/one';chatPendingFiles=[{hash:'first'}];chatSaveDraft();
chatDraftKey='claude/two';input.value='Draft for second session';chatPendingFiles=[];chatSaveDraft();
assert.equal(chatDrafts.get('codex/one').text,'Draft for first session');
assert.equal(chatDrafts.get('claude/two').text,'Draft for second session');
assert.equal(chatDrafts.get('codex/one').files[0].hash,'first');
`
	if out, err := exec.Command(node, "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
