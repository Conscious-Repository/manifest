const test = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');

function fixture() {
  const storage = new Map(), timers = new Map(), requests = [];
  let now = 0, id = 0, version = 1;
  const ctx = vm.createContext({
    TextEncoder, TextDecoder, AbortSignal, crypto: require('node:crypto').webcrypto,
    localStorage: {setItem:(k,v)=>storage.set(k,v), removeItem:k=>storage.delete(k)},
    window: {addEventListener(){}}, document: {addEventListener(){}, hidden:false},
    setTimeout: (fn, ms) => {timers.set(++id, {fn, at:now+ms}); return id},
    clearTimeout: id => timers.delete(id), queueMicrotask,
    save: async (url, body) => {requests.push({url, body}); return {revision:'r'+(++version)}},
    fetch: async () => ({status:304}),
  });
  const run = code => vm.runInContext(code, ctx);
  run(fs.readFileSync('server/web/js/71-write.js', 'utf8'));
  run(String.raw`writeFetch=save;writeStatus=()=>{};writeConflict=()=>{};writeMark=()=>{};writeRenderComments=()=>{};showToast=()=>{};
    writingUI.vaultID='fixture';
    var d={path:'note.md',base:'start',revision:'r1',eol:'\n',comments:{revision:'c1'},editor:{
      value:'start',text(){return this.value},view:{composing:false},
      syncText(text){this.value=text;writeChanged(d)},setReadOnly(value){this.readOnly=value}
    }};writingUI.documents.set(d.path,d);`);
  const settle = async () => {for(let i=0;i<20;i++)await Promise.resolve()};
  return {ctx, run, requests, storage, timers, settle,
    type(text) {ctx.typed=text; run('d.editor.value=typed;writeChanged(d)')},
    async tick(ms) {now+=ms;for(const [key,t] of [...timers])if(t.at<=now){timers.delete(key);t.fn()}await settle()},
    remote(raw, revision='r2') {ctx.fetch=async()=>({ok:true,status:200,json:async()=>({raw,revision})})},
  };
}

test('typing autosaves once after a pause and removes the acknowledged recovery draft', async () => {
  const f=fixture();f.type('one');await f.tick(400);f.type('one two');await f.tick(799);
  assert.equal(f.requests.length,0);await f.tick(1);
  assert.equal(f.requests.length,1);assert.equal(f.requests[0].body.body,'one two');
  assert.equal(f.run('writeDirty(d)'),false);assert.equal(f.storage.size,0);
});

test('continuous typing is saved within five seconds', async () => {
  const f=fixture();f.type('0');
  for(let i=1;i<=10;i++){await f.tick(500);f.type(String(i))}
  assert.equal(f.requests.length,1);assert.equal(f.requests[0].body.body,'9');
});

test('typing during a save is automatically sent with the acknowledged revision', async () => {
  const f=fixture();let finish;
  f.ctx.save=(url,body)=>{f.requests.push({url,body});return new Promise(r=>finish=r)};
  f.run('writeFetch=save');f.type('first');await f.tick(800);f.type('second');
  finish({revision:'r2'});await f.settle();await f.tick(800);
  assert.equal(f.requests.length,2);assert.equal(f.requests[1].body.ifRevision,'r2');
  assert.equal(f.requests[1].body.body,'second');finish({revision:'r3'});await f.settle();
  assert.equal(f.run('writeDirty(d)'),false);
});

test('explicit save joins an autosave and flushes newer typing', async () => {
  const f=fixture();let finish;
  f.ctx.save=()=>new Promise(r=>finish=r);f.run('writeFetch=save');
  f.type('first');await f.tick(800);f.type('newer');
  const explicit=f.run('writeSave(d)');finish({revision:'r2'});await f.settle();
  finish({revision:'r3'});assert.equal(await explicit,true);
  assert.equal(f.run('d.base'),'newer');
});

test('offline failure retains the draft and retries with its original revision', async () => {
  const f=fixture();f.ctx.save=async()=>{throw new TypeError('offline')};f.run('writeFetch=save');
  f.type('offline writing');await f.tick(800);
  assert.equal(f.run('d.revision'),'r1');assert.equal(f.storage.size,1);
  f.ctx.save=async(url,body)=>{f.requests.push({url,body});return {revision:'r2'}};f.run('writeFetch=save');
  await f.tick(2000);assert.equal(f.requests[0].body.ifRevision,'r1');
  assert.equal(f.run('writeDirty(d)'),false);assert.equal(f.storage.size,0);
});

test('lost save acknowledgement accepts identical server bytes without an overwrite', async () => {
  const f=fixture();f.ctx.save=async()=>{throw Object.assign(new Error('conflict'),{status:409,detail:{raw:'submitted',revision:'r2'}})};f.run('writeFetch=save');
  f.type('submitted');await f.tick(800);
  assert.equal(f.run('d.revision'),'r2');assert.equal(f.run('d.error'),'');
  assert.equal(f.run('writeDirty(d)'),false);assert.equal(f.storage.size,0);
});

test('clean external changes update the buffer without a save echo', async () => {
  const f=fixture();f.remote('🌿\r\nremote\r\n');await f.run('writeRefresh(d)');await f.tick(5000);
  assert.equal(f.run('d.editor.text()'),'🌿\nremote\n');assert.equal(f.run('d.eol'),'\r\n');
  assert.equal(f.run('writeDirty(d)'),false);assert.equal(f.requests.length,0);
});

test('remote response arriving after a save cannot roll the editor back', async () => {
  const f=fixture();let finish;f.ctx.fetch=()=>new Promise(r=>finish=r);
  const refresh=f.run('writeRefresh(d)');f.type('mine');await f.tick(800);
  finish({ok:true,status:200,json:async()=>({raw:'old remote',revision:'old'})});await refresh;
  assert.equal(f.run('d.base'),'mine');assert.equal(f.run('d.editor.text()'),'mine');
});

test('typing during a refresh prevents the stale response from replacing or conflicting with it', async () => {
  const f=fixture();let finish;f.ctx.fetch=()=>new Promise(r=>finish=r);
  const refresh=f.run('writeRefresh(d)');f.type('mine');
  finish({ok:true,status:200,json:async()=>({raw:'external',revision:'r2'})});await refresh;
  assert.equal(f.run('d.editor.text()'),'mine');assert.equal(f.run('d.conflict'),undefined);
});

test('competing external edits stop autosave and preserve both versions for review', async () => {
  const f=fixture();f.type('my draft');f.remote('their edit');await f.run('writeRefresh(d)');
  f.type('more of my draft');await f.tick(10000);
  assert.equal(f.requests.length,0);assert.equal(f.run('d.conflict.raw'),'their edit');
  assert.equal(f.run('d.editor.text()'),'more of my draft');
  assert.equal(JSON.parse([...f.storage.values()][0]).text,'more of my draft');
});

test('a deleted note is never automatically recreated', async () => {
  const f=fixture();f.ctx.fetch=async()=>({ok:false,status:404});await f.run('writeRefresh(d)');
  f.type('keep this');await f.tick(10000);
  assert.equal(f.run('d.conflict.missing'),true);assert.equal(f.requests.length,0);
});

test('restoring an externally deleted file safely resumes a retained draft', async () => {
  const f=fixture();f.ctx.fetch=async()=>({ok:false,status:404});await f.run('writeRefresh(d)');
  f.type('retained draft');f.remote('start','r1');await f.run('writeRefresh(d)');await f.tick(800);
  assert.equal(f.run('d.conflict'),null);assert.equal(f.requests[0].body.ifRevision,'r1');
  assert.equal(f.requests[0].body.body,'retained draft');
});

test('closed documents and moves reject late refreshes', async () => {
  for(const mutation of ['d.closed=true','d.path="moved.md"','d.moving=true']){
    const f=fixture();let finish;f.ctx.fetch=()=>new Promise(r=>finish=r);
    const refresh=f.run('writeRefresh(d)');f.run(mutation);
    finish({ok:true,status:200,json:async()=>({raw:'late',revision:'r2'})});await refresh;
    assert.equal(f.run('d.editor.text()'),'start');
  }
});

test('autosave pauses for IME composition, read-only notes and recovery choices', async () => {
  for(const flag of ['d.editor.view.composing','d.readOnly','d.recovering']){
    const f=fixture();f.run(flag+'=true');f.type('writing');await f.tick(5000);
    assert.equal(f.requests.length,0);
    f.run(flag+'=false;writeQueueSave(d)');await f.tick(800);assert.equal(f.requests.length,1);
  }
});

test('comment creation reanchors its unchanged quotation after autosave', async () => {
  const f=fixture();f.run(`d.base='start quote';d.editor.value=d.base;d.pending={revision:'r1',start:6,end:11,quote:'quote'};d.pendingBody='question';`);
  f.type('new start quote');await f.tick(800);
  await f.run('writePost(d,{body:d.pendingBody,anchor:d.pending})');
  const comment=f.requests.find(r=>r.url==='/api/writing/comments');
  assert.equal(comment.body.anchor.revision,'r2');assert.equal(comment.body.anchor.start,10);
  assert.equal(comment.body.anchor.end,15);assert.equal(f.run('d.pending'),null);
});

test('a changed quotation retains the unsent comment', async () => {
  const f=fixture();f.run(`d.pending={revision:'r1',start:0,end:5,quote:'start'};d.pendingBody='question';`);
  f.type('different');await f.tick(800);
  await f.run('writePost(d,{body:d.pendingBody,anchor:d.pending})');
  assert.equal(f.requests.filter(r=>r.url==='/api/writing/comments').length,0);
  assert.equal(f.run('d.pendingBody'),'question');
});
