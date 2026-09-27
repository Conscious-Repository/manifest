// ui-compare.cjs — the virtual browser tool (tools/ui-compare) end to end on
// two local pages with known differences: every width is captured for both
// sides, the report pairs them, and the summary names the design-token
// differences (typeface, text size, radius, control height) and our page's
// horizontal overflow at phone width. A missing page fails in words.
const {execFileSync}=require('node:child_process'),fs=require('node:fs'),os=require('node:os'),path=require('node:path'),assert=require('node:assert/strict');
const dir=fs.mkdtempSync(path.join(os.tmpdir(),'ui-compare-'));
const ours=path.join(dir,'ours.html'),ref=path.join(dir,'ref.html'),out=path.join(dir,'out');
fs.writeFileSync(ours,'<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width"><style>body{font:18px/1.7 Georgia,serif;margin:0;padding:16px}button{border-radius:2px;padding:2px 6px}.wide{width:620px;height:10px;background:#eee}</style><h1>Chat</h1><p>Hello from ours, a paragraph of reading text.</p><button>Send</button><div class="wide"></div>');
fs.writeFileSync(ref,'<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width"><style>body{font:14px/1.5 Arial,sans-serif;margin:0;padding:16px}button{border-radius:12px;padding:12px 16px}</style><h1>Chat</h1><p>Hello from the reference, a paragraph of reading text.</p><button>Send</button>');
const tool=path.join(__dirname,'../../tools/ui-compare/ui-compare.cjs');
const log=execFileSync(process.execPath,[tool,'compare','file://'+ours,'file://'+ref,'--widths','390,1280','--out',out,'--wait','50','--selector','button'],{encoding:'utf8'});
assert.match(log,/wrote .*summary\.md/);
for(const f of ['ours-390.png','ours-1280.png','reference-390.png','reference-1280.png','ours-390-crop.png','report.html','summary.md','tokens.json'])assert.ok(fs.existsSync(path.join(out,f)),'missing '+f);
const summary=fs.readFileSync(path.join(out,'summary.md'),'utf8');
assert.match(summary,/primary typeface: ours Georgia · reference Arial/);
assert.match(summary,/body text size: ours 18px · reference 14px/);
assert.match(summary,/most common radius: ours 2px · reference 12px/);
assert.match(summary,/median control height/);
assert.match(summary,/ours overflows horizontally by \d+px; the reference does not/);
const html=fs.readFileSync(path.join(out,'report.html'),'utf8');
assert.ok(html.includes("src='ours-390.png'")&&html.includes("src='reference-390.png'"),'report does not pair the screenshots');
// a bot check is named, never bypassed; a screenshot can stand in for the reference
const http=require('node:http');
const srv=http.createServer((q,r)=>{r.writeHead(403,{'Content-Type':'text/html'});r.end('<title>Just a moment...</title><p>Verify you are human</p>');});
srv.listen(0,'127.0.0.1',()=>{});
const port=()=>srv.address().port;
const waitListen=()=>new Promise(r=>srv.listening?r():srv.on('listening',r));
(async()=>{await waitListen();
 // async: the stub server lives in this process and must keep answering
 const blocked=await new Promise(res=>require('node:child_process').execFile(process.execPath,[tool,'compare','file://'+ours,'http://127.0.0.1:'+port()+'/','--widths','390','--out',path.join(dir,'blocked'),'--wait','0'],{encoding:'utf8'},e=>res(e)));
 srv.close();
 assert.ok(blocked===null,'one blocked side of two is not a total failure');
 assert.match(fs.readFileSync(path.join(dir,'blocked','summary.md'),'utf8'),/BLOCKED: the site served a bot check \(HTTP 403\).*screenshot instead/);
 const img=path.join(out,'reference-1280.png');
 execFileSync(process.execPath,[tool,'compare','file://'+ours,img,'--widths','390,1280','--out',path.join(dir,'image'),'--wait','0'],{encoding:'utf8'});
 const s2=fs.readFileSync(path.join(dir,'image','summary.md'),'utf8');
 assert.match(s2,/owner-supplied screenshot/);assert.match(s2,/not measured: the reference is a screenshot/);
 assert.ok(fs.readFileSync(path.join(dir,'image','report.html'),'utf8').includes("src='reference.png'"));
 finish();
})().catch(e=>{console.error(e);process.exitCode=1;});
function finish(){
// a page that cannot load fails in words, and the exit says so
let failed=null;try{execFileSync(process.execPath,[tool,'shot','http://127.0.0.1:9/nothing-here','--widths','390','--out',path.join(dir,'bad'),'--wait','0'],{encoding:'utf8',stdio:'pipe'});}catch(e){failed=e;}
assert.ok(failed&&failed.status===1,'an unreachable page must exit 1');
assert.match(fs.readFileSync(path.join(dir,'bad','summary.md'),'utf8'),/FAILED: /);
console.log('PASS: both sides captured at every width, paired report, token differences, overflow, crop, bot check named, screenshot reference, failure in words.');
}
