// WRITING — preview and export (owner, 2026-10-06, after iA Writer): a
// preview beside the text (or instead of it; on a phone always instead),
// following the editor's scroll, in one of three templates: Modern (the
// app's sans), Classic (serif) or Manuscript (mono, double spaced). Content
// blocks embed another vault file named alone on its line. Export makes
// HTML, PDF (the browser's print to PDF) or Word (.docx, built here), and
// Markdown as before. The renderer (markdown-it) loads on first use.

let writePreviewLoad=null;
function writePreviewReady(){return writePreviewLoad||=writeLoadScript('vendor/writing-preview.js').then(()=>window.ManifestMarkdown)}
const writeTemplates={modern:'Modern',classic:'Classic',manuscript:'Manuscript'};
const writeTemplateFonts={modern:'"Hanken Grotesk", "Helvetica Neue", Arial, sans-serif',classic:'Charter, "Iowan Old Style", Georgia, "Times New Roman", serif',manuscript:'"Spline Sans Mono", "JetBrains Mono", "Courier New", monospace'};
const writeEsc=s=>String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));

// the page's look: on screen it takes the app's colours; on paper, ink on white
function writeTemplateCSS(paper){
  const t=writePrefs.template in writeTemplates?writePrefs.template:'modern',root=getComputedStyle(document.documentElement);
  const v=n=>root.getPropertyValue(n).trim();
  const c=paper?{bg:'#ffffff',text:'#1a1a1a',muted:'#666666',line:'#d8d8d8',accent:'#1f5fbf'}:{bg:v('--bg')||'#fff',text:v('--text')||'#171717',muted:v('--muted')||'#666',line:v('--line')||'#ddd',accent:v('--accent')||'#265acc'};
  const fontFace=writeFontFaces();
  const fonts={...writeTemplateFonts,modern:(v('--sans')||writeTemplateFonts.modern)+', '+writeTemplateFonts.modern};
  return fontFace+`
:root{--bg:${c.bg};--text:${c.text};--muted:${c.muted};--line:${c.line};--accent:${c.accent};color-scheme:${paper?'light':'light dark'}}
html{background:var(--bg);color:var(--text)}
body{margin:0;padding:40px 24px 120px;-webkit-text-size-adjust:100%}
article{max-width:${t==='manuscript'?'38em':'40em'};margin:0 auto;font:${t==='manuscript'?'16px/2':'18px/1.65'} ${fonts[t]};overflow-wrap:break-word}
h1,h2,h3,h4,h5,h6{line-height:1.25;margin:1.6em 0 .6em;font-weight:${t==='classic'?'600':'700'}}
h1{font-size:${t==='manuscript'?'1.4em':'2em'};margin-top:.4em}h2{font-size:${t==='manuscript'?'1.2em':'1.45em'}}h3{font-size:1.15em}h4,h5,h6{font-size:1em}
${writePrefs.centerHeadings?'h1,h2,h3,h4,h5,h6{text-align:center}':''}
${writePrefs.numberHeadings?'article{counter-reset:h2}h2{counter-reset:h3}h2::before{counter-increment:h2;content:counter(h2) ". "}h3::before{counter-increment:h3;content:counter(h2) "." counter(h3) " "}':''}
${writePrefs.indentParagraphs?'p+p{text-indent:1.5em;margin-top:-.4em}':''}
p,ul,ol,blockquote,pre,table,figure{margin:0 0 1em}
a{color:var(--accent)}a.wikilink.missing{color:var(--muted);text-decoration-style:dotted}
blockquote{margin-left:0;padding-left:1em;border-left:3px solid var(--line);color:var(--muted)}
code{font:.88em "Spline Sans Mono","JetBrains Mono",ui-monospace,monospace;background:color-mix(in srgb,var(--text) 7%,transparent);padding:.1em .3em;border-radius:3px}
pre{overflow:auto;padding:.9em 1em;background:color-mix(in srgb,var(--text) 6%,transparent);border-radius:6px}pre code{background:none;padding:0}
mark{background:color-mix(in srgb,#ffd400 45%,transparent);color:inherit;padding:0 .1em}
hr{border:0;border-top:1px solid var(--line);margin:2em 0}
.page-break{border-top:1px dashed var(--line);margin:2em 0}
li.task{list-style:none;margin-left:-1.3em}li.task input{margin:0 .45em 0 0}
table{border-collapse:collapse;width:100%;font-size:.92em}th,td{border:1px solid var(--line);padding:.35em .6em;text-align:left;vertical-align:top}
img{max-width:100%;height:auto}figure{margin:1.2em 0}figcaption{color:var(--muted);font-size:.88em;margin-top:.4em}
.content-block.missing{color:var(--muted);font-style:italic}
.toc ul{list-style:none;padding:0}.toc li{margin:.2em 0}.toc-3{padding-left:1.2em}.toc-4,.toc-5,.toc-6{padding-left:2.4em}
.footnotes{font-size:.88em;color:var(--muted)}.footnotes-sep{margin-top:3em}
.title-page{min-height:60vh;display:flex;flex-direction:column;justify-content:center;text-align:center;break-after:page}.title-page h1{font-size:2.4em}.title-page p{color:var(--muted)}
@media print{body{padding:0}article{max-width:none}.page-break{border:0;margin:0;break-after:page}a{color:inherit;text-decoration:none}@page{margin:2.2cm 2cm}}
`;}

// the app's own @font-face rules, with their files' full addresses, so the
// preview (and an exported page opened from this server) sets the same type
let writeFontCSS=null;
function writeFontFaces(){
  if(writeFontCSS!==null)return writeFontCSS;
  const out=[];
  for(const sheet of document.styleSheets){let rules;try{rules=sheet.cssRules}catch(e){continue}
    for(const r of rules||[])if(r instanceof CSSFontFaceRule){const src=r.style.getPropertyValue('src').replace(/url\((['"]?)([^'")]+)\1\)/g,(m,q,u)=>'url("'+new URL(u,sheet.href||location.href).href+'")');out.push('@font-face{'+r.style.cssText.replace(/src:[^;]+;?/,'')+'src:'+src+'}')}}
  return writeFontCSS=out.join('\n');
}
// frontmatter: its keys (title, author…) feed exports; the body is what renders
function writeSplitMeta(text){
  const m=/^---\n([\s\S]*?)\n(?:---|\.\.\.)(?:\n|$)/.exec(text);if(!m)return {meta:{},body:text};
  const meta={};for(const line of m[1].split('\n')){const kv=/^([\w-]+)\s*:\s*(.*)$/.exec(line);if(kv)meta[kv[1].toLowerCase()]=kv[2].replace(/^["']|["']$/g,'')}
  return {meta,body:text.slice(m[0].length)};
}

// ---- content blocks: resolved before the (synchronous) render ----
let writeAssetList=null;
async function writeAssets(){if(!writeAssetList){try{writeAssetList=(await writeFetch('/api/writing/assets')).assets||[]}catch(e){writeAssetList=[]}}return writeAssetList}
function writeBlockFiles(d){
  const out=writingUI.files.map(f=>({path:f.path,kind:'note'}));if(writeAssetList)out.push(...writeAssetList);else writeAssets();
  return out;
}
function writeResolveBlock(d,path){
  const dir=d.path.includes('/')?d.path.slice(0,d.path.lastIndexOf('/')+1):'';
  for(const p of [dir+path,path]){
    const note=writingUI.files.find(f=>f.path===p||f.path===p+'.md');if(note)return {kind:'note',path:note.path};
    const asset=(writeAssetList||[]).find(a=>a.path===p);if(asset)return asset;
  }
  return null;
}
function writeCSVTable(text){
  const sep=text.includes('\t')&&!text.includes(',')?'\t':',';
  const rows=text.trim().split(/\r?\n/).map(line=>{const out=[];let cur='',q=false;for(let i=0;i<line.length;i++){const ch=line[i];if(q){if(ch==='"'&&line[i+1]==='"'){cur+='"';i++}else if(ch==='"')q=false;else cur+=ch}else if(ch==='"')q=true;else if(ch===sep){out.push(cur);cur=''}else cur+=ch}out.push(cur);return out});
  if(!rows.length)return '';
  return '<table><thead><tr>'+rows[0].map(c=>'<th>'+writeEsc(c)+'</th>').join('')+'</tr></thead><tbody>'+rows.slice(1).map(r=>'<tr>'+r.map(c=>'<td>'+writeEsc(c)+'</td>').join('')+'</tr>').join('')+'</tbody></table>';
}
async function writeBlocksFor(d,body,embed){
  const lines=[...body.matchAll(/^\/(\S+?)(?:\s+(?:"[^"]*"|\([^)]*\)))?\s*$/gm)].map(m=>m[1]);
  if(lines.length)await writeAssets();
  const blocks=new Map(),md=window.ManifestMarkdown;
  for(const path of [...new Set(lines)]){
    const hit=writeResolveBlock(d,path);if(!hit)continue;
    try{
      if(hit.kind==='note'){const note=await writeFetch('/api/note?path='+encodeURIComponent(hit.path));blocks.set(path,{kind:'note',html:md.render(writeSplitMeta(note.raw).body,{link:t=>writeLinkHref(t),wikilinks:embed?'text':'link'}).html})}
      else if(hit.kind==='image'){let src='/api/writing/asset?path='+encodeURIComponent(hit.path);if(embed){const blob=await (await fetch(src)).blob();src=await new Promise(r=>{const fr=new FileReader();fr.onload=()=>r(fr.result);fr.readAsDataURL(blob)})}blocks.set(path,{kind:'image',src})}
      else{const text=await (await fetch('/api/writing/asset?path='+encodeURIComponent(hit.path))).text();blocks.set(path,{kind:hit.kind,text})}
    }catch(e){}
  }
  return (path,caption)=>{const b=blocks.get(path);if(!b)return null;const cap=caption?'<figcaption>'+writeEsc(caption)+'</figcaption>':'';
    if(b.kind==='image')return '<figure class="content-block"><img src="'+writeEsc(b.src)+'" alt="'+writeEsc(caption||path)+'">'+cap+'</figure>\n';
    if(b.kind==='csv')return '<figure class="content-block">'+writeCSVTable(b.text)+cap+'</figure>\n';
    if(b.kind==='note')return '<section class="content-block">'+b.html+'</section>\n';
    return '<figure class="content-block"><pre><code>'+writeEsc(b.text)+'</code></pre>'+cap+'</figure>\n'};
}
function writeLinkHref(target){
  const want=target.split('#')[0].trim().toLowerCase().replace(/\.md$/i,'');
  const f=writingUI.files.find(x=>x.path.replace(/\.md$/i,'').toLowerCase()===want)||writingUI.files.find(x=>x.name.toLowerCase()===want.replace(/^.*\//,''));
  return f?{href:'#/write/'+encodeURIComponent(f.path)}:{href:'#',missing:true};
}
// one render: the article's HTML plus what an export needs
async function writeRender(d,{embed=false}={}){
  const md=await writePreviewReady();
  const {meta,body}=writeSplitMeta(d.editor.text());
  const block=await writeBlocksFor(d,body,embed);
  const out=md.render(body,{link:t=>writeLinkHref(t),block,wikilinks:embed?'text':'link'});
  const title=meta.title||out.headings.find(h=>h.level===1)?.title||d.path.replace(/^.*\//,'').replace(/\.md$/i,'');
  return {html:out.html,title,meta};
}

// ---- the pane ----
function writePreviewBuild(){
  if(writingUI.previewPane)return writingUI.previewPane;
  const pane=el('section','write-preview');pane.setAttribute('aria-label','Preview');
  const bar=el('div','write-preview-bar');
  const tpl=el('button','write-preview-tool','');tpl.type='button';tpl.onclick=()=>writeTemplateMenu(tpl);
  const exp=el('button','write-preview-tool','Export');exp.type='button';exp.onclick=()=>writeExportMenu(exp);
  const close=writeIcon('×','Close preview',()=>writeSetPref('preview','off'));
  bar.append(tpl,exp,close);
  const frame=document.createElement('iframe');frame.className='write-preview-frame';frame.title='Preview';frame.setAttribute('sandbox','allow-same-origin allow-modals');
  frame.srcdoc='<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><style id="tpl"></style></head><body><article></article></body></html>';
  frame.onload=()=>{const doc=frame.contentDocument;doc.addEventListener('click',e=>{const a=e.target.closest?.('a');if(!a)return;const href=a.getAttribute('href')||'';e.preventDefault();
    if(href.startsWith('#/write/'))location.hash=href;else if(href.startsWith('#')&&href.length>1)doc.getElementById(decodeURIComponent(href.slice(1)))?.scrollIntoView({behavior:'smooth'});else if(/^https?:/.test(href))window.open(href,'_blank','noopener')});
    writingUI.previewReady=true;writePreviewRefresh(true)};
  pane.append(bar,frame);
  Object.assign(writingUI,{previewPane:pane,previewFrame:frame,previewTemplate:tpl});
  return pane;
}
function writeEffectivePreview(){const p=writePrefs.preview;return p==='split'&&window.mf?.phone?.()?'full':p}
function writeApplyPreview(){
  const work=writingUI.work;if(!work)return;
  const mode=writingUI.active?writeEffectivePreview():'off';
  if(mode!=='off'){const pane=writePreviewBuild();if(pane.parentElement!==work)work.insertBefore(pane,writingUI.margin)}
  writingUI.view.dataset.preview=mode;
  writingUI.preview?.setAttribute('aria-pressed',String(mode!=='off'));
  if(writingUI.previewTemplate)writingUI.previewTemplate.textContent=(writeTemplates[writePrefs.template]||'Modern')+' ⌄';
  if(mode!=='off')writePreviewRefresh(true);
}
function writeTogglePreview(){writeSetPref('preview',writePrefs.preview==='off'?(window.mf?.phone?.()?'full':'split'):'off');return true}
let writePreviewTimer=0;
function writePreviewRefresh(now){
  clearTimeout(writePreviewTimer);
  const run=async()=>{
    const d=writingUI.active,frame=writingUI.previewFrame;if(!d||!frame||!writingUI.previewReady||writeEffectivePreview()==='off')return;
    const doc=frame.contentDocument;if(!doc)return;
    try{const {html}=await writeRender(d);if(writingUI.active!==d)return;
      doc.getElementById('tpl').textContent=writeTemplateCSS(false);
      const article=doc.querySelector('article');if(article.innerHTML!==html)article.innerHTML=html;
      writePreviewFollow(d);
    }catch(e){doc.querySelector('article').textContent=e.message}
  };
  if(now)run();else writePreviewTimer=setTimeout(run,250);
}
// the preview keeps the editor's first visible line at its top
function writePreviewFollow(d){
  const frame=writingUI.previewFrame;if(!frame?.contentDocument||writeEffectivePreview()!=='split')return;
  const line=d.editor.topLine(),doc=frame.contentDocument;let target=null;
  for(const n of doc.querySelectorAll('article > [data-line]')){if(+n.dataset.line<=line)target=n;else break}
  frame.contentWindow.scrollTo({top:target?Math.max(0,target.offsetTop-24):0});
}
writeOnEdit.push(()=>{if(writeEffectivePreview()!=='off')writePreviewRefresh()});
writeOnMount.push(d=>{
  writeApplyPreview();
  if(d.previewScroll)return;d.previewScroll=true;
  let frame=0;d.editor.view.scrollDOM.addEventListener('scroll',()=>{if(frame||writingUI.active!==d)return;frame=requestAnimationFrame(()=>{frame=0;writePreviewFollow(d)})},{passive:true});
});
async function writeTemplateMenu(anchor){
  const items=[{header:'Template'},...Object.entries(writeTemplates).map(([k,label])=>({label,checked:writePrefs.template===k,run:()=>writeSetPref('template',k)})),{header:'Options'},
    {label:'Center headings',checked:!!writePrefs.centerHeadings,run:()=>writeSetPref('centerHeadings',!writePrefs.centerHeadings)},
    {label:'Number headings',checked:!!writePrefs.numberHeadings,run:()=>writeSetPref('numberHeadings',!writePrefs.numberHeadings)},
    {label:'Indent paragraphs',checked:!!writePrefs.indentParagraphs,run:()=>writeSetPref('indentParagraphs',!writePrefs.indentParagraphs)},
    {label:'Title page in PDF',checked:!!writePrefs.titlePage,run:()=>writeSetPref('titlePage',!writePrefs.titlePage)}];
  (await chooseActionMenu(anchor,items,'Template'))?.run();
}
async function writeExportMenu(anchor){
  const d=writingUI.active;if(!d)return;
  const choice=await chooseActionMenu(anchor,writeExportItems(d),'Export');choice?.run();
}
function writeExportItems(d){return [{label:'PDF…',run:()=>writeExportPDF(d)},{label:'Word (.docx)',run:()=>writeExportDocx(d)},{label:'HTML',run:()=>writeExportHTML(d)},{label:'Markdown',run:()=>writeExport(d)}]}
writeMenuHooks.push(d=>[{header:'Export'},...writeExportItems(d).filter(i=>i.label!=='Markdown').map(i=>({label:'export '+i.label,run:i.run}))]);

// ---- exports ----
function writeFileName(d,ext){return d.path.replace(/^.*\//,'').replace(/\.md$/i,'')+'.'+ext}
function writeDownload(name,blob){const url=URL.createObjectURL(blob);const a=el('a');a.href=url;a.download=name;document.body.append(a);a.click();a.remove();setTimeout(()=>URL.revokeObjectURL(url),2000)}
async function writePaperDoc(d){
  const {html,title,meta}=await writeRender(d,{embed:true});
  const front=writePrefs.titlePage?'<section class="title-page"><h1>'+writeEsc(title)+'</h1>'+(meta.author?'<p>'+writeEsc(meta.author)+'</p>':'')+(meta.date?'<p>'+writeEsc(meta.date)+'</p>':'')+'</section>':'';
  return {title,meta,html,doc:'<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>'+writeEsc(title)+'</title>'+(meta.author?'<meta name="author" content="'+writeEsc(meta.author)+'">':'')+'<style>'+writeTemplateCSS(true)+'</style></head><body><article>'+front+html+'</article></body></html>'};
}
async function writeExportHTML(d){try{const {doc}=await writePaperDoc(d);writeDownload(writeFileName(d,'html'),new Blob([doc],{type:'text/html;charset=utf-8'}))}catch(e){showToast(e.message,null,'error')}}
async function writeExportPDF(d){
  try{
    const {doc}=await writePaperDoc(d);
    const frame=document.createElement('iframe');frame.className='write-print-frame';frame.setAttribute('aria-hidden','true');
    frame.srcdoc=doc;document.body.append(frame);
    await new Promise(r=>frame.onload=r);await frame.contentDocument.fonts?.ready;
    const imgs=[...frame.contentDocument.images].filter(i=>!i.complete);await Promise.all(imgs.map(i=>new Promise(r=>{i.onload=i.onerror=r})));
    frame.contentWindow.focus();frame.contentWindow.print();
    setTimeout(()=>frame.remove(),60000);
  }catch(e){showToast(e.message,null,'error')}
}
async function writeExportDocx(d){
  try{const {html,title,meta}=await writePaperDoc(d);writeDownload(writeFileName(d,'docx'),writeDocx(html,title,meta))}catch(e){showToast(e.message,null,'error')}
}

// ---- Word: the rendered article as WordprocessingML, zipped here ----
function writeDocx(html,title,meta){
  const dom=new DOMParser().parseFromString('<body>'+html+'</body>','text/html').body;
  const x=s=>String(s).replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c])).replace(/[\u0000-\u0008\u000B\u000C\u000E-\u001F]/g,'');
  const links=[],lists=[];let body='';
  const rPr=f=>{const p=(f.b?'<w:b/>':'')+(f.i?'<w:i/>':'')+(f.s?'<w:strike/>':'')+(f.code?'<w:rFonts w:ascii="Courier New" w:hAnsi="Courier New"/><w:sz w:val="20"/>':'')+(f.mark?'<w:highlight w:val="yellow"/>':'')+(f.sup?'<w:vertAlign w:val="superscript"/>':'')+(f.link?'<w:rStyle w:val="Hyperlink"/>':'');return p?'<w:rPr>'+p+'</w:rPr>':''};
  const run=(text,f)=>text?'<w:r>'+rPr(f)+'<w:t xml:space="preserve">'+x(text)+'</w:t></w:r>':'';
  function runs(node,f={}){
    let out='';
    for(const n of node.childNodes){
      if(n.nodeType===3){out+=run(n.textContent.replace(/\s+/g,' '),f);continue}
      if(n.nodeType!==1)continue;const tag=n.tagName.toLowerCase();
      if(tag==='br'){out+='<w:r><w:br/></w:r>';continue}
      if(tag==='input'){out+=run(n.checked?'☑ ':'☐ ',f);continue}
      if(tag==='img'){out+=run('['+(n.getAttribute('alt')||'image')+']',{...f,i:true});continue}
      if(['ul','ol','table','pre','blockquote','figure','section','div','p','h1','h2','h3','h4','h5','h6','nav','hr'].includes(tag))continue;
      const g={...f};if(tag==='strong'||tag==='b')g.b=true;if(tag==='em'||tag==='i')g.i=true;if(tag==='s'||tag==='del')g.s=true;if(tag==='code')g.code=true;if(tag==='mark')g.mark=true;if(tag==='sup')g.sup=true;
      if(tag==='a'&&/^https?:/.test(n.getAttribute('href')||'')){links.push(n.getAttribute('href'));out+='<w:hyperlink r:id="rIdL'+links.length+'">'+runs(n,{...g,link:true})+'</w:hyperlink>';continue}
      out+=runs(n,g);
    }
    return out;
  }
  const para=(inner,style,extra='')=>'<w:p><w:pPr>'+(style?'<w:pStyle w:val="'+style+'"/>':'')+extra+'</w:pPr>'+inner+'</w:p>';
  function blocks(node,ctx={}){
    for(const n of node.children){
      const tag=n.tagName.toLowerCase();
      if(/^h[1-6]$/.test(tag))body+=para(runs(n),'Heading'+tag[1]);
      else if(tag==='p')body+=para(runs(n),ctx.quote?'Quote':ctx.list?null:null,ctx.list||'');
      else if(tag==='blockquote')blocks(n,{...ctx,quote:true});
      else if(tag==='pre')for(const line of n.textContent.replace(/\n$/,'').split('\n'))body+=para(run(line,{code:true}),'Code');
      else if(tag==='ul'||tag==='ol'){
        const level=(ctx.level??-1)+1;let id=ctx.numId;
        if(tag==='ol'||!id){lists.push(tag==='ol'?'decimal':'bullet');id=lists.length}
        for(const li of n.children){const numPr='<w:numPr><w:ilvl w:val="'+Math.min(level,8)+'"/><w:numId w:val="'+id+'"/></w:numPr>';
          const inline=[...li.childNodes].filter(c=>c.nodeType===3||!['P','UL','OL','PRE','BLOCKQUOTE'].includes(c.tagName));
          const holder=document.createElement('span');inline.forEach(c=>holder.append(c.cloneNode(true)));
          const ps=[...li.children].filter(c=>c.tagName==='P');
          body+=para(runs(holder)+(ps[0]?runs(ps[0]):''),'ListParagraph',numPr);
          for(const p of ps.slice(1))body+=para(runs(p),'ListParagraph','<w:ind w:left="'+(720*(level+1))+'"/>');
          for(const sub of [...li.children].filter(c=>c.tagName==='UL'||c.tagName==='OL')){const w=document.createElement('div');w.append(sub.cloneNode(true));blocks(w,{...ctx,level,numId:sub.tagName==='UL'?id:undefined})}
        }
      }
      else if(tag==='hr')body+=para('',null,'<w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="AAAAAA"/></w:pBdr>');
      else if(n.classList.contains('page-break'))body+='<w:p><w:r><w:br w:type="page"/></w:r></w:p>';
      else if(tag==='table'){
        body+='<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/><w:tblW w:w="0" w:type="auto"/><w:tblBorders>'+['top','left','bottom','right','insideH','insideV'].map(s=>'<w:'+s+' w:val="single" w:sz="4" w:color="BBBBBB"/>').join('')+'</w:tblBorders></w:tblPr>';
        for(const tr of n.querySelectorAll('tr')){body+='<w:tr>';for(const cell of tr.children)body+='<w:tc><w:tcPr><w:tcW w:w="0" w:type="auto"/></w:tcPr>'+para(runs(cell,{b:cell.tagName==='TH'}))+'</w:tc>';body+='</w:tr>'}
        body+='</w:tbl>'+para('');
      }
      else if(tag==='figure'){blocks(n,ctx);const cap=n.querySelector('figcaption');const img=n.querySelector(':scope > img');if(img)body+=para(run('['+(img.alt||'image')+']',{i:true}))}
      else if(tag==='figcaption')body+=para(runs(n),'Caption');
      else blocks(n,ctx);
    }
  }
  blocks(dom);
  const W='xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"';
  const font=writePrefs.template==='classic'?'Georgia':writePrefs.template==='manuscript'?'Courier New':'Arial';
  const style=(id,name,extra,type='paragraph')=>'<w:style w:type="'+type+'" w:styleId="'+id+'"><w:name w:val="'+name+'"/>'+(type==='paragraph'&&id!=='Normal'?'<w:basedOn w:val="Normal"/><w:next w:val="Normal"/>':'')+extra+'</w:style>';
  const heading=(n,size)=>style('Heading'+n,'heading '+n,'<w:pPr><w:keepNext/><w:spacing w:before="'+(n<3?360:240)+'" w:after="120"/><w:outlineLvl w:val="'+(n-1)+'"/></w:pPr><w:rPr><w:b/><w:sz w:val="'+size+'"/></w:rPr>');
  const styles='<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:styles '+W+'><w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="'+font+'" w:hAnsi="'+font+'" w:cs="'+font+'"/><w:sz w:val="22"/></w:rPr></w:rPrDefault><w:pPrDefault><w:pPr><w:spacing w:after="160" w:line="'+(writePrefs.template==='manuscript'?480:300)+'" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>'
    +style('Normal','Normal','')+heading(1,36)+heading(2,30)+heading(3,26)+heading(4,24)+heading(5,22)+heading(6,22)
    +style('Quote','Quote','<w:pPr><w:ind w:left="567"/></w:pPr><w:rPr><w:i/><w:color w:val="555555"/></w:rPr>')
    +style('Code','Code','<w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/><w:shd w:val="clear" w:fill="F3F3F3"/></w:pPr><w:rPr><w:rFonts w:ascii="Courier New" w:hAnsi="Courier New"/><w:sz w:val="19"/></w:rPr>')
    +style('ListParagraph','List Paragraph','<w:pPr><w:spacing w:after="60"/></w:pPr>')+style('Caption','caption','<w:rPr><w:i/><w:color w:val="666666"/><w:sz w:val="19"/></w:rPr>')
    +style('Hyperlink','Hyperlink','<w:rPr><w:color w:val="1F5FBF"/><w:u w:val="single"/></w:rPr>','character')+style('TableGrid','Table Grid','','table')+'</w:styles>';
  const lvl=(kind,i)=>'<w:lvl w:ilvl="'+i+'"><w:start w:val="1"/><w:numFmt w:val="'+kind+'"/><w:lvlText w:val="'+(kind==='bullet'?['•','◦','▪'][i%3]:'%'+(i+1)+'.')+'"/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="'+(720*(i+1))+'" w:hanging="360"/></w:pPr></w:lvl>';
  const numbering='<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:numbering '+W+'>'+lists.map((k,i)=>'<w:abstractNum w:abstractNumId="'+i+'">'+Array.from({length:9},(_,l)=>lvl(k,l)).join('')+'</w:abstractNum>').join('')+lists.map((k,i)=>'<w:num w:numId="'+(i+1)+'"><w:abstractNumId w:val="'+i+'"/></w:num>').join('')+'</w:numbering>';
  const doc='<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document '+W+'><w:body>'+body+'<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1247" w:right="1134" w:bottom="1247" w:left="1134" w:header="708" w:footer="708" w:gutter="0"/></w:sectPr></w:body></w:document>';
  const rels='<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rIdS" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/><Relationship Id="rIdN" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>'
    +links.map((h,i)=>'<Relationship Id="rIdL'+(i+1)+'" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="'+x(h)+'" TargetMode="External"/>').join('')+'</Relationships>';
  const now=new Date().toISOString().replace(/\.\d+Z$/,'Z');
  const core='<?xml version="1.0" encoding="UTF-8" standalone="yes"?><cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"><dc:title>'+x(title)+'</dc:title>'+(meta.author?'<dc:creator>'+x(meta.author)+'</dc:creator>':'')+'<dcterms:created xsi:type="dcterms:W3CDTF">'+now+'</dcterms:created></cp:coreProperties>';
  const types='<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/><Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/><Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/></Types>';
  const root='<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/></Relationships>';
  return writeZip([['[Content_Types].xml',types],['_rels/.rels',root],['word/document.xml',doc],['word/styles.xml',styles],['word/numbering.xml',numbering],['word/_rels/document.xml.rels',rels],['docProps/core.xml',core]],'application/vnd.openxmlformats-officedocument.wordprocessingml.document');
}
// a stored (uncompressed) zip: enough for a .docx, no library needed
let writeCRCTable=null;
function writeCRC(bytes){writeCRCTable||=Array.from({length:256},(_,n)=>{let c=n;for(let k=0;k<8;k++)c=c&1?0xEDB88320^(c>>>1):c>>>1;return c>>>0});let c=0xFFFFFFFF;for(const b of bytes)c=writeCRCTable[(c^b)&0xFF]^(c>>>8);return (c^0xFFFFFFFF)>>>0}
function writeZip(entries,type){
  const enc=new TextEncoder(),parts=[],central=[];let offset=0;
  for(const [name,text] of entries){
    const data=enc.encode(text),nm=enc.encode(name),crc=writeCRC(data);
    const head=new DataView(new ArrayBuffer(30));[[0,0x04034b50,4],[4,20,2],[6,0x0800,2],[8,0,2],[10,0,2],[12,0x21,2],[14,crc,4],[18,data.length,4],[22,data.length,4],[26,nm.length,2],[28,0,2]].forEach(([o,v,s])=>s===4?head.setUint32(o,v,true):head.setUint16(o,v,true));
    parts.push(new Uint8Array(head.buffer),nm,data);
    const cd=new DataView(new ArrayBuffer(46));[[0,0x02014b50,4],[4,20,2],[6,20,2],[8,0x0800,2],[10,0,2],[12,0,2],[14,0x21,2],[16,crc,4],[20,data.length,4],[24,data.length,4],[28,nm.length,2],[30,0,2],[32,0,2],[34,0,2],[36,0,2],[38,0,4],[42,offset,4]].forEach(([o,v,s])=>s===4?cd.setUint32(o,v,true):cd.setUint16(o,v,true));
    central.push(new Uint8Array(cd.buffer),nm);offset+=30+nm.length+data.length;
  }
  const size=central.reduce((n,p)=>n+p.length,0),end=new DataView(new ArrayBuffer(22));
  [[0,0x06054b50,4],[4,0,2],[6,0,2],[8,entries.length,2],[10,entries.length,2],[12,size,4],[16,offset,4],[20,0,2]].forEach(([o,v,s])=>s===4?end.setUint32(o,v,true):end.setUint16(o,v,true));
  return new Blob([...parts,...central,new Uint8Array(end.buffer)],{type});
}
