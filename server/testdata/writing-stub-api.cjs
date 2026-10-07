// writing-stub-api.cjs — a stateful stub of the Writing API for browser
// fixtures that load the real front end (index.html + every script). An
// in-memory vault (makeStub({files:{path:text}}) or a small default), exact
// revisions as ETags, and the iA-style surfaces: search (a simplified form of
// the server's syntax: terms, "phrases", -exclusions, #tags, [ ] / [x]),
// tags, folders, the library document and authorship sidecars.
// Hooks: stub.files (path → text), stub.library, stub.authorship, stub.log.
const fs=require('node:fs'),path=require('node:path'),http=require('node:http'),crypto=require('node:crypto');
const web=path.join(__dirname,'../web');
const types={'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml','.png':'image/png','.woff2':'font/woff2','.ttf':'font/ttf','.json':'application/json'};
const rev=t=>crypto.createHash('sha256').update(t).digest('hex');
const defaults={
 'essays/on writing.md':'# On writing\n\nWriting is thinking. It is basically the slowest way to find out what you mean, and that is the point.\n\n## Why plain text\n\nPlain text lasts. #craft\n\n- a list item that runs on long enough to wrap onto a second line at a phone width, so its wrap can be measured\n- [ ] an open task\n\nSee [[ideas]] and [[missing note]].\n',
 'ideas.md':'# Ideas\n\nA place for #craft and #research ideas.\n\n- [x] a done task\n',
 'journal/2026-10-01.md':'Morning pages. Nothing much.\n',
 'draft.md':'---\ntitle: Draft\nauthor: Ben\n---\nA draft with frontmatter.\n',
 'preview.md':'---\ntitle: Preview test\n---\n# Preview test\n\n{{TOC}}\n\n## Lists\n\n- [x] done\n- [ ] open\n- plain with **bold**, *italic*, ==mark== and `code`[^1]\n\n1. one\n2. two\n\n> A quote.\n\n## Blocks\n\n/data.csv "Numbers"\n\n/ideas\n\n+++\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\nSee [[ideas]] and [[nowhere]] and https://example.com.\n\n[^1]: A footnote.\n',
};
function tagsOf(text){const out=[];const body=text.replace(/^---\n[\s\S]*?\n---\n/,'').replace(/```[\s\S]*?```/g,'').replace(/`[^`]*`/g,'');
 for(const m of body.matchAll(/(^|[\s(])#([\p{L}\p{N}_]*\p{L}[\p{L}\p{N}_]*)/gu))out.push(m[2]);return out}
function excerptOf(text){return text.replace(/^---\n[\s\S]*?\n---\n/,'').replace(/^#+\s.*$/m,'').replace(/[#*_`>\[\]-]/g,'').replace(/\s+/g,' ').trim().slice(0,160)}
function makeStub(opts={}){
 const files=new Map(Object.entries(opts.files||defaults));
 const assets=opts.assets||{'data.csv':'n,square\n2,4\n3,9\n'};
 const folders=new Set(opts.folders||[]);
 const times=new Map([...files.keys()].map((p,i)=>[p,1790000000+i*1000]));
 let library={},libraryRev='';const authorship=new Map(),comments=new Map(),log=[];
 const json=(res,code,body)=>{res.writeHead(code,{'Content-Type':'application/json'});res.end(JSON.stringify(body));};
 const body=req=>new Promise(r=>{let b='';req.on('data',c=>b+=c);req.on('end',()=>{try{r(JSON.parse(b||'{}'))}catch(e){r({})}})});
 const allFolders=()=>{const set=new Set(['',...folders]);for(const p of files.keys()){const parts=p.split('/');for(let i=1;i<parts.length;i++)set.add(parts.slice(0,i).join('/'))}return [...set].sort()};
 const entry=p=>({path:p,name:p.replace(/^.*\//,'').replace(/\.md$/,''),modified:times.get(p),readOnly:false,excerpt:excerptOf(files.get(p)),tags:[...new Set(tagsOf(files.get(p)))]});
 function search(q){
  const tokens=q.match(/-?"[^"]*"|\[[ xX]\]|\S+/g)||[];
  const tests=tokens.map(t=>{const neg=t.startsWith('-')&&t.length>1;const v=neg?t.slice(1):t;let f;
   if(v==='#'){f=(p,x)=>!tagsOf(x).length}
   else if(v.startsWith('"'))f=(p,x)=>x.toLowerCase().includes(v.slice(1,-1).toLowerCase());
   else if(v.startsWith('#'))f=(p,x)=>tagsOf(x).some(g=>g.toLowerCase()===v.slice(1).toLowerCase());
   else if(/^\[ \]$/.test(v))f=(p,x)=>/^\s*[-*+] \[ \]/m.test(x);
   else if(/^\[[xX]\]$/.test(v))f=(p,x)=>/^\s*[-*+] \[[xX]\]/m.test(x);
   else if(v.startsWith('name:'))f=p=>p.replace(/^.*\//,'').toLowerCase().includes(v.slice(5).toLowerCase());
   else f=(p,x)=>new RegExp('(^|[^\\p{L}\\p{N}])'+v.replace(/[.*+?^${}()|[\]\\]/g,'\\$&'),'iu').test(p.replace(/^.*\//,'')+'\n'+x);
   return neg?(p,x)=>!f(p,x):f;});
  return [...files.entries()].filter(([p,x])=>tests.every(t=>t(p,x))).map(([p,x])=>({...entry(p),snippet:excerptOf(x),score:1}));
 }
 const server=http.createServer(async(req,res)=>{
  const url=new URL(req.url,'http://x');const p=url.pathname;
  if(p.startsWith('/api/')){
   log.push(req.method+' '+p+url.search);
   // other surfaces' reads, for fixtures that borrow this server: {path: body}
   if(req.method==='GET'&&opts.json&&Object.prototype.hasOwnProperty.call(opts.json,p))return json(res,200,opts.json[p]);
   if(p==='/api/writing/files')return json(res,200,{files:[...files.keys()].sort().map(entry),folders:allFolders(),vaultID:'stubvault'});
   if(p==='/api/note'&&req.method==='GET'){const q=url.searchParams.get('path');if(!files.has(q))return json(res,404,{missing:true});const r=rev(files.get(q));if(req.headers['if-none-match']==='"'+r+'"'){res.writeHead(304);return res.end()}return json(res,200,{path:q,raw:files.get(q),revision:r,vaultID:'stubvault',readOnly:false})}
   if(p==='/api/note'&&req.method==='PUT'){const b=await body(req);if(!files.has(b.path))return json(res,409,{missing:true});const cur=files.get(b.path);if(rev(cur)!==b.ifRevision)return json(res,409,{revision:rev(cur),raw:cur});files.set(b.path,b.body);times.set(b.path,Math.floor(Date.now()/1000));return json(res,200,{revision:rev(b.body)})}
   if(p==='/api/writing/note'){const b=await body(req);if(files.has(b.path))return json(res,409,{error:'exists'});const dir=b.path.includes('/')?b.path.slice(0,b.path.lastIndexOf('/')):'';if(dir&&!allFolders().includes(dir)){res.writeHead(400);return res.end('folder does not exist')}files.set(b.path,b.body||'');times.set(b.path,Math.floor(Date.now()/1000));return json(res,200,{path:b.path,revision:rev(b.body||'')})}
   if(p==='/api/writing/folder'){const b=await body(req);if(!b.path||/(^|\/)\.\.?(\/|$)|^\//.test(b.path)){res.writeHead(400);return res.end('bad folder')}folders.add(b.path);return json(res,200,{path:b.path})}
   if(p==='/api/writing/move'){const b=await body(req);const t=files.get(b.path);files.delete(b.path);files.set(b.to,t);times.set(b.to,times.get(b.path));if(authorship.has(b.path)){authorship.set(b.to,authorship.get(b.path));authorship.delete(b.path)}return json(res,200,{path:b.to})}
   if(p==='/api/writing/comments'&&req.method==='GET'){const q=url.searchParams.get('path');return json(res,200,{document:comments.get(q)||{revision:'',threads:[],turns:[]},agentAvailable:true})}
   if(p==='/api/writing/search'){const q=(url.searchParams.get('q')||'').trim();if(!q){res.writeHead(400);return res.end('empty query')}const results=search(q);return json(res,200,{results,total:results.length})}
   if(p==='/api/writing/tags'){const count=new Map();for(const x of files.values())for(const t of new Set(tagsOf(x)))count.set(t,(count.get(t)||0)+1);return json(res,200,{tags:[...count].map(([tag,count])=>({tag,count})).sort((a,b)=>b.count-a.count||a.tag.localeCompare(b.tag))})}
   if(p==='/api/writing/library'&&req.method==='GET')return json(res,200,{library,revision:libraryRev});
   if(p==='/api/writing/library'&&req.method==='PUT'){const b=await body(req);if((b.ifRevision||'')!==libraryRev)return json(res,409,{library,revision:libraryRev});library=b.library||{};libraryRev=rev(JSON.stringify(library));return json(res,200,{revision:libraryRev})}
   if(p==='/api/writing/authorship'&&req.method==='GET')return json(res,200,authorship.get(url.searchParams.get('path'))||{ranges:[],authors:[],revision:''});
   if(p==='/api/writing/authorship'&&req.method==='PUT'){const b=await body(req);authorship.set(url.searchParams.get('path'),{revision:b.revision||'',ranges:b.ranges||[],authors:b.authors||[]});return json(res,200,{ok:true})}
   if(p==='/api/writing/assets')return json(res,200,{assets:[...Object.keys(assets)].map(p=>({path:p,kind:/\.(png|svg|jpe?g|gif|webp)$/i.test(p)?'image':/\.(csv|tsv)$/i.test(p)?'csv':'text'}))});
   if(p==='/api/writing/asset'){const a=assets[url.searchParams.get('path')];if(a==null){res.writeHead(404);return res.end()}res.writeHead(200,{'Content-Type':/\.svg$/.test(url.searchParams.get('path'))?'image/svg+xml':'text/plain'});return res.end(a)}
   if(p==='/api/note/resolve')return json(res,200,{kind:'missing',target:url.searchParams.get('target')});
   if(p==='/api/terminal/events'){res.writeHead(200,{'Content-Type':'text/event-stream','Cache-Control':'no-store'});return res.end('retry: 86400000\n\n');}
   const shell={'/api/feed/badge':{count:0},'/api/tasks':{outstanding:[],assignees:{},counts:{tasks:0}},'/api/goals':{areas:[]},'/api/aion':{backlog:[]},'/api/properties':{properties:[],deals:[],templates:[],holdings:{}},'/api/re/backlog':{items:[],goalsArea:null},'/api/settings/connections':{rows:[]}};
   if(req.method==='GET'&&shell[p])return json(res,200,shell[p]);
   return json(res,404,{});
  }
  const file=path.join(web,p==='/'?'index.html':p);
  if(!file.startsWith(web)||!fs.existsSync(file)||fs.statSync(file).isDirectory()){res.writeHead(404);return res.end();}
  res.writeHead(200,{'Content-Type':types[path.extname(file)]||'application/octet-stream'});fs.createReadStream(file).pipe(res);
 });
 return {server,files,folders,log,get library(){return library},authorship};
}
module.exports={makeStub};
