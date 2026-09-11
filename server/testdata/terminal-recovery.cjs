// Actual frontend control flow with deterministic time, inventory, and sockets.
const assert = require('node:assert/strict'), fs = require('node:fs'), vm = require('node:vm'), path = require('node:path');
const source = fs.readFileSync(path.join(__dirname, '../web/js/73-terminal.js'), 'utf8');
const stable = {id:'fixture', live:true, backend:'herdr', runtime:{host:'host',generation:'g',session:'s',workspace:'w',pane:'p',occupant:'o',agentSession:'a'}};
function fixture() {
  let now = 0;
  let timers = new Map(), next = 1, sockets = [], inventory = [stable], offline = false, calls = [];
  const element = () => ({append(){},replaceChildren(){},classList:{add(){},remove(){}},clientWidth:100,clientHeight:100});
  const nodes = {termScreen:element(),termConnection:{textContent:''}};
  const ctx = vm.createContext({AbortSignal,console,Date:{now:()=>now},cmdRegistry:{register(){}},localStorage:{getItem(){}},
    document:{hidden:false,documentElement:{},getElementById:id=>nodes[id],querySelector:()=>null},
    window:{innerWidth:1000},els:{terminalView:{hidden:false}},location:{protocol:'https:',host:'fixture'},
    getComputedStyle:()=>({getPropertyValue:()=>''}),el:element,showToast:()=>{},
    setTimeout:(fn,delay)=>{const id=next++;timers.set(id,{fn,delay});return id;},clearTimeout:id=>timers.delete(id),
    fetch:async(url,opts)=>{calls.push(url);assert.equal(url,'/api/terminal/live');if(offline)throw Error('outage');return {ok:true,json:async()=>({enabled:true,connectivity:'connected',sessions:inventory})};},
    WebSocket:class{constructor(url){this.url=url;this.readyState=0;this.sent=[];sockets.push(this);}send(data){this.sent.push(JSON.parse(data));}close(){this.readyState=3;this.onclose?.();}},
    Terminal:class{constructor(){this.cols=80;this.rows=24;this.parser={registerOscHandler(){}};}loadAddon(){}open(){}write(){}dispose(){}focus(){}onData(){}},
    FitAddon:{FitAddon:class{fit(){}}},ResizeObserver:class{observe(){}disconnect(){}}
  });
  vm.runInContext(source + '\ntermRenderControls=()=>{};renderTermSessions=()=>{};renderTermLauncher=()=>{};', ctx);
  const run = code=>vm.runInContext(code,ctx);
  return {ctx,sockets,calls,timers,run,status:()=>nodes.termConnection.textContent,
    offline:value=>offline=value,inventory:value=>inventory=value,
    async tick(){const [id,timer]=timers.entries().next().value;timers.delete(id);await timer.fn();return timer.delay;},
    async start(){await run("termOpenId='fixture';loadTermSessions()");this.open();},
    open(stable=true){const ws=sockets.at(-1);ws.readyState=1;ws.onopen();if(stable)now+=10000;},
    close(){sockets.at(-1).close();}};
}
(async()=>{
  let f=fixture();await f.start();f.offline(true);f.close();
  assert.equal(await f.tick(),1200);assert.equal(f.sockets.length,1);assert.equal(f.timers.size,1,'first failed inventory must schedule another retry');
  assert.equal(await f.tick(),2400);f.offline(false);await f.tick();assert.equal(f.sockets.length,2);f.open();assert.equal(f.status(),'Connected');assert.equal(f.timers.size,0);
  // Repeat outage; an SSE inventory refresh also recovers before the next timer.
  f.close();f.offline(true);await f.tick();f.offline(false);await f.run('loadTermSessions(true)');assert.equal(f.sockets.length,3);f.open();
  assert.ok(f.sockets.every(s=>s.sent.every(frame=>frame.t==='r')),'no input replay');
  f.close();f.offline(true);const delays=[];while(f.timers.size)delays.push(await f.tick());
  assert.deepEqual(delays,[1200,2400,4800,9600,15000,15000]);assert.equal(f.status(),'Disconnected · use Reconnect');
  f.offline(false);await f.run('loadTermSessions(true)');assert.equal(f.sockets.length,4,'restored daemon revives exhausted recovery');f.open();
  // Hidden browser, hidden view and Files stage pause; refresh on return resumes.
  for(const hide of ['document.hidden','els.terminalView.hidden','termStage']) {
    f=fixture();await f.start();f.close();f.run(hide+(hide==='termStage'?"='files'":'=true'));await f.tick();assert.equal(f.timers.size,0);assert.equal(f.sockets.length,1);
    await f.run('loadTermSessions(true)');assert.equal(f.sockets.length,1);
    f.run(hide+(hide==='termStage'?"='term'":'=false'));await f.run('loadTermSessions(true)');assert.equal(f.sockets.length,2);f.open();
  }
  f=fixture();await f.start();f.close();f.run('detachTerm()');await f.run('loadTermSessions(true)');assert.equal(f.timers.size,0);assert.equal(f.sockets.length,1,'explicit detach survives quiet refresh');
  for(const field of ['host','generation','session','workspace','pane','occupant','agentSession']) {
    f=fixture();await f.start();f.close();f.inventory([{...stable,runtime:{...stable.runtime,[field]:'replacement'}}]);await f.tick();await f.run('loadTermSessions(true)');assert.equal(f.sockets.length,1,field);assert.equal(f.timers.size,0);assert.match(f.status(),/Pane changed/);
  }
  f=fixture();await f.start();f.close();f.inventory([stable,{...stable,id:'second'}]);await f.run("termOpenId='second';loadTermSessions()");f.open();assert.equal(f.timers.size,0);assert.match(f.sockets[1].url,/id=second/);
  // Late old inventory completion after detach cannot resurrect the client.
  f=fixture();await f.start();f.close();let resolve;f.ctx.fetch=()=>new Promise(r=>resolve=r);const pending=f.tick();f.run('detachTerm()');resolve({ok:true,json:async()=>({connectivity:'connected',sessions:[stable]})});await pending;assert.equal(f.sockets.length,1);assert.equal(f.timers.size,0);
  f=fixture();await f.start();f.close();f.inventory([]);await f.tick();assert.equal(f.run('termInst'),null);assert.equal(f.timers.size,0,'ended pane cannot launch');
  // Repeated handshake failures also exhaust, without resetting on construction.
  f=fixture();await f.start();f.close();for(let i=0;i<6;i++){await f.tick();f.close();}assert.equal(f.timers.size,0);assert.equal(f.sockets.length,7);assert.match(f.status(),/use Reconnect/);
  f=fixture();await f.run("termOpenId='fixture';loadTermSessions()");assert.equal(await f.tick(),10000);assert.equal(f.run('termInst.retry.state'),'waiting','hung handshake is bounded');
  f=fixture();await f.start();f.close();for(let i=0;i<6;i++){await f.tick();f.open(false);f.close();}assert.equal(f.timers.size,0);assert.match(f.status(),/use Reconnect/,'short-lived HTTP upgrades must not reset budget');
  f=fixture();await f.start();const buttons=[],bar={dataset:{},append:b=>buttons.push(b)};const get=f.ctx.document.getElementById;f.ctx.document.getElementById=id=>id==='termKeys'?bar:get(id);f.ctx.el=(tag,cls,text)=>({text});f.run('buildTermKeys()');
  buttons.find(b=>b.text==='tab').onclick();buttons.find(b=>b.text==='shift-tab').onclick();assert.deepEqual(f.sockets[0].sent.filter(frame=>frame.t==='i').map(frame=>frame.d),['\t','\x1b[Z'],'raw Terminal supports both keys through its separate byte transport');
  console.log('PASS terminal recovery: backoff, exhaustion, daemon return, hidden/navigation, identity, detach, handshake, no replay');
})().catch(e=>{console.error(e);process.exitCode=1;});
