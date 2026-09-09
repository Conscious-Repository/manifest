package server

import (
	"os/exec"
	"strings"
	"testing"
)

func TestTerminalFrontendEventDrivenStateContract(t *testing.T) {
	src := readWebJS(t, "web/js/48-chat.js")
	if strings.Contains(src, "chatTermPollTimer") || strings.Contains(src, "ensureChatTermPoll") {
		t.Fatal("coding rail interval polling returned")
	}
	tick := jsBody(t, src, "async function chatTermTick() {")
	if strings.Contains(tick, "!o.live") {
		t.Fatal("stopped process hides final transcript")
	}
	for _, fn := range []string{"async function chatTermScreenFetch() {", "async function chatTermTail(o) {"} {
		if !strings.Contains(jsBody(t, src, fn), `o.se.backend !== "herdr"`) {
			t.Fatalf("%s may overwrite herdr SSE state from file/screen replies", fn)
		}
	}
	tasks := readWebJS(t, "web/js/90-todos.js")
	if !strings.Contains(jsBody(t, tasks, "function delegationChip(d, asSpan, taskId) {"), "terminalRunBadge(d)") {
		t.Fatal("task badges no longer share terminal stream")
	}
	if !strings.Contains(jsBody(t, src, "async function renderTaskChat(taskID, refetch) {"), "terminalRunBadge(d.delegation)") {
		t.Fatal("task Chat head lost run link")
	}
}

func TestTerminalFrontendDisconnectAndFinalTranscript(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable; browser behavior fixture needs JavaScript runtime")
	}
	src := readWebJS(t, "web/js/48-chat.js")
	script := `
const assert = require('node:assert/strict');
let sourceCount = 0, intervalCount = 0, reads = 0;
const badges = [];
global.EventSource = class {
 constructor(url) { assert.equal(url, '/api/terminal/events'); sourceCount++; this.listeners = {}; }
 addEventListener(name, cb) { this.listeners[name] = cb; }
};
global.setInterval = () => { intervalCount++; return 1; };
global.document = { addEventListener(){}, hidden: false, querySelectorAll: () => badges, getElementById: () => null, querySelector: () => null };
global.els = { chatView: { hidden: false } };
global.fmtWhen = value => value;
global.cmdRegistry = {register(){}};
global.window = {addEventListener(){},dispatchEvent(){}};
global.CustomEvent = class { constructor(name,init){this.type=name;this.detail=init.detail;} };
global.el = (tag, cls, text) => ({ textContent: text || '', children: [], dataset: {}, classList: {add(){}}, setAttribute(){}, replaceChildren(...nodes){this.children=nodes;}, append(...nodes){this.children.push(...nodes);} });
global.statusDot = (on, title) => Object.assign(el('span', '', ''), {on, title});
global.fetch = async (url) => { if (url.includes('/transcript')) reads++; return { json: async () => ({ live: true, lines: [], turns: [], offset: 0 }) }; };
` + src + `
renderChatRail = () => {};
chatTermRepaintHead = () => {};
renderChatComposer = () => {};
chatTermPaintStrip = () => {};
chatTermPaintTurns = () => {};
(async () => {
 chatTermSessions = [{id:'s',backend:'herdr',kind:'codex',name:'test'}];
 ensureTerminalEvents(); ensureTerminalEvents();
 assert.equal(sourceCount,1); assert.equal(intervalCount,0);
 const emit = (state, process='running', connected=true) => terminalEvents.listeners.state({data:JSON.stringify({connected,sessions:[{manifestId:'s',runId:'r',identity:{backend:'herdr',pane:'p',occupant:'t'},agentState:state,connectivity:'connected',process,observedAt:'2026-09-09T01:00:00Z'}]})});
 emit('working');
 assert.equal(chatTermSessions[0].agentState,'working'); assert.equal(chatTermSessions[0].live,true);
 let openedPaneSession; chatTermOpenInTerminal = se => { openedPaneSession = se.id; };
 const badge = terminalRunBadge({harness:'codex',runId:'r'}); badges.push(badge);
 badge.children[2].onclick({preventDefault(){},stopPropagation(){}}); assert.equal(openedPaneSession,'s');
 assert.equal(sourceCount,1); assert.equal(badge.children[1].textContent,'agent working');
 chatAgent='codex'; chatOpenId='s';
 const o = chatTermOpen = {id:'s',se:chatTermSessions[0],live:true,offset:0,turns:[],screen:[],screenSig:''};
 terminalEvents.onerror();
 assert.equal(chatTermSessions[0].agentState,'unknown'); assert.equal(o.live,false);
 assert.equal(badge.children[1].textContent,'agent unavailable'); assert.equal(badge.children.length,2,'unavailable pane must not offer attach');
 await new Promise(setImmediate);
 // A late pre-disconnect response cannot resurrect working/process liveness.
 await chatTermTail(o); await chatTermScreenFetch();
 assert.equal(o.live,false); assert.equal(o.se.agentState,'unknown');
 emit('working'); await new Promise(setImmediate);
 const beforeStop=reads;
 document.hidden=true;
 emit('unknown','stopped'); await new Promise(setImmediate);
 assert.ok(reads>beforeStop,'stop must force final file read even while hidden');
 assert.equal(o.live,false); assert.equal(o.se.process,'stopped');
 document.hidden=false;
 const beforeTick=reads; await chatTermTick();
 assert.ok(reads>beforeTick,'normal file reconciliation must continue after stop');
 assert.equal(intervalCount,0,'state updates must never install polling');
 emit('done');
 assert.equal(badge.children[1].textContent,'agent done');
 assert.equal(badge.title,'runtime observation; the run report determines task status');
 // File tails stay serialized: stop during an in-flight read queues a final read.
 let release;
 global.fetch = async (url) => {
   if (url.includes('/transcript')) { reads++; if (!release) await new Promise(resolve => { release=resolve; }); }
   return {json:async()=>({live:true,turns:[],offset:0,lines:[]})};
 };
 const beforePending=reads; const pending=chatTermTick();
 emit('unknown','stopped');
 release(); await pending; await new Promise(setImmediate);
 assert.ok(reads>=beforePending+2,'stop during read must queue another final tail');
})().catch(err=>{console.error(err);process.exitCode=1;});
`
	cmd := exec.Command(node, "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("terminal browser fixture: %v\n%s", err, out)
	}
}
