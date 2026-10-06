// writing-ia.cjs — Writing after iA Writer (owner, 2026-10-06), on the real
// front end over the writing stub (writing-stub-api.cjs):
//   editor   heading marks hang (a heading starts on the body's text edge),
//            a list item's wrapped line starts under its text, syntax is
//            dimmed but present; focus dims all but the sentence; line length
//            and text size apply; the stats bubble cycles; a missing [[link]]
//            offers to create its note;
//   library  organizer → section → file; search with #tags; favorites stored
//            in the library document; a phone shows one screen at a time;
//   quick    ⌘O lists the outline, Enter jumps; > lists commands;
//   tools    Style Check strikes a filler; Syntax Highlight colours nouns;
//            a paste is marked and its authorship stored beside the note;
//   preview  side by side with a table of contents and a content block; Word
//            export is a well-formed .docx carrying the text.
// Run: NODE_PATH=<node_modules with playwright> node server/testdata/writing-ia.cjs
const {chromium}=require('playwright'),assert=require('node:assert/strict'),zlib=require('node:zlib');
const {makeStub}=require('./writing-stub-api.cjs');
(async()=>{
 const stub=makeStub();await new Promise(r=>stub.server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+stub.server.address().port;
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chromium'});
 const open=async(route,w=1440)=>{const ctx=await browser.newContext({viewport:{width:w,height:w<861?844:900},isMobile:w<861,hasTouch:w<861,acceptDownloads:true});const page=await ctx.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));await page.goto(base+route);await page.waitForTimeout(900);return {ctx,page,errors}};
 try{
  // ---- editor ----
  if(process.env.STEPS)console.error('editor');
  {const {ctx,page,errors}=await open('/#/write/essays%2Fon%20writing.md');
   await page.locator('.cm-content').click();
   const g=await page.evaluate(()=>{const left=l=>{const w=document.createTreeWalker(l,NodeFilter.SHOW_TEXT);let n;while((n=w.nextNode())){if(n.parentElement.closest('.write-hmark,.write-lmark')||!n.textContent.trim())continue;const r=document.createRange(),i=n.textContent.search(/\S/);r.setStart(n,i);r.setEnd(n,i+1);return r.getBoundingClientRect()}};
     const lines=[...document.querySelectorAll('.cm-line')],h1=lines.find(l=>l.textContent.startsWith('# ')),body=lines.find(l=>l.textContent.startsWith('Writing is')),li=lines.find(l=>l.classList.contains('write-li'));
     const range=document.createRange();range.selectNodeContents(li);const rects=[...range.getClientRects()].filter(r=>r.width>0),top=Math.min(...rects.map(r=>r.top));const rows=rects.filter(r=>r.top>top+8).map(r=>Math.round(r.left));
     return {h1:left(h1).left,body:left(body).left,hmark:getComputedStyle(h1.querySelector('.write-hmark')).color===getComputedStyle(document.querySelector('.write-path')).color||!!h1.querySelector('.write-hmark'),liText:left(li).left,liRows:rows,measure:document.querySelector('.cm-content').getBoundingClientRect().width}});
   assert.ok(Math.abs(g.h1-g.body)<=2,'the heading starts on the text edge: '+g.h1+' vs '+g.body);
   assert.ok(g.hmark,'the heading mark is present');
   assert.ok(g.liRows.length&&Math.abs(Math.min(...g.liRows)-g.liText)<=2,'the list item wraps under its text: '+JSON.stringify(g));
   await page.keyboard.press('Control+d');assert.ok(await page.locator('.write-dim').count()>0,'focus dims the rest');
   await page.keyboard.press('Control+d');assert.equal(await page.locator('.write-dim').count(),0,'focus off');
   await page.evaluate(()=>writeSetPref('measure',64));const narrow=await page.evaluate(()=>document.querySelector('.cm-content').getBoundingClientRect().width);
   assert.ok(narrow<g.measure-30,'64 characters is narrower: '+narrow+' vs '+g.measure);
   await page.evaluate(()=>writeSetPref('size','lg'));assert.equal(await page.evaluate(()=>getComputedStyle(document.querySelector('.cm-editor')).fontSize),'19px');
   await page.evaluate(()=>{writeSetPref('size','md');writeSetPref('measure',72)});
   const before=await page.locator('.write-count').textContent();await page.locator('.write-count').click();
   assert.match(before,/words$/);assert.match(await page.locator('.write-count').textContent(),/characters$/);await page.evaluate(()=>writeSetPref('stat','words'));
   if(process.env.STEPS)console.error(' link');
   // a missing link offers to create its note
   await page.evaluate(()=>{writeFollowLink(writingUI.active,'missing note')});
   await page.getByRole('menuitem',{name:/Create “missing note”/}).click();
   await page.waitForFunction(()=>location.hash==='#/write/'+encodeURIComponent('essays/missing note.md'));
   assert.ok(stub.files.has('essays/missing note.md'),'the note was created beside the linking one');
   assert.deepEqual(errors,[]);await ctx.close();}
  // ---- library ----
  if(process.env.STEPS)console.error('library');
  {const {ctx,page,errors}=await open('/#/write');
   const labels=await page.locator('.write-lib-section .write-lib-name').allTextContents();
   for(const want of ['Recents','All files','Favorites','essays','journal','#craft'])assert.ok(labels.includes(want),'organizer lists '+want+': '+labels);
   await page.locator('.write-lib-section',{hasText:'essays'}).click();await page.waitForFunction(()=>location.hash.includes('~list/folder')&&document.querySelectorAll('.write-lib-file').length===2);
   assert.deepEqual(await page.locator('.write-lib-file .write-lib-name').allTextContents(),['missing note','on writing'],'newest first');
   await page.locator('.write-lib-search').fill('#craft');await page.waitForFunction(()=>document.querySelectorAll('.write-lib-file').length===2);
   await page.locator('.write-lib-search').fill('');
   await page.locator('.write-lib-file',{hasText:'on writing'}).click({button:'right'});await page.getByRole('menuitem',{name:'Add to favorites'}).click();
   await page.waitForFunction(()=>writeLib.doc.favorites?.length===1);assert.deepEqual(stub.library.favorites,['essays/on writing.md']);
   assert.deepEqual(errors,[]);await ctx.close();}
  // ---- quick search ----
  if(process.env.STEPS)console.error('quick search');
  {const {ctx,page,errors}=await open('/#/write/essays%2Fon%20writing.md');
   await page.locator('.cm-content').click();await page.keyboard.press('Control+o');
   await page.keyboard.type('plain');await page.waitForSelector('.write-quick-row');
   assert.equal(await page.locator('.write-quick-row').first().textContent(),'## Why plain text');
   await page.keyboard.press('Enter');
   assert.equal(await page.evaluate(()=>{const d=writingUI.active;const t=d.editor.text();return t.slice(0,d.editor.selection().head).split('\n').length}),5,'the caret is on the heading');
   await page.keyboard.press('Control+Shift+P');await page.waitForSelector('.write-quick-row');
   assert.equal(await page.locator('.write-quick .cmdbar-input').inputValue(),'>');assert.ok((await page.locator('.write-quick-row').count())>10,'commands listed');
   await page.keyboard.press('Escape');
   assert.deepEqual(errors,[]);await ctx.close();}
  // ---- editing tools ----
  if(process.env.STEPS)console.error('editing tools');
  {const {ctx,page,errors}=await open('/#/write/essays%2Fon%20writing.md');
   await page.locator('.cm-content').click();
   await page.evaluate(()=>writeSetPref('style',true));await page.waitForSelector('.write-style');
   assert.equal(await page.locator('.write-style').first().textContent(),'basically');
   await page.evaluate(()=>writeSetPref('syntax',true));await page.waitForSelector('.write-pos-noun',{timeout:10000});
   await page.keyboard.press('Control+End');await page.keyboard.press('Enter');
   await page.evaluate(()=>{const dt=new DataTransfer();dt.setData('text/plain','Words from elsewhere.');document.querySelector('.cm-content').dispatchEvent(new ClipboardEvent('paste',{clipboardData:dt,bubbles:true}))});
   await page.evaluate(()=>writeSetPref('authors',true));await page.waitForSelector('.write-author-pasted');
   await page.waitForFunction(()=>true);await page.waitForTimeout(2500);
   const saved=stub.authorship.get('essays/on writing.md');assert.ok(saved&&saved.ranges.some(r=>r.author==='pasted'&&r.quote==='Words from elsewhere.'),'authorship stored: '+JSON.stringify(saved));
   assert.ok(!stub.files.get('essays/on writing.md').includes('pasted'),'no mark in the file');
   assert.deepEqual(errors,[]);await ctx.close();}
  // ---- preview and export ----
  if(process.env.STEPS)console.error('preview and export');
  {const {ctx,page,errors}=await open('/#/write/preview.md');
   await page.keyboard.press('Control+Alt+p');await page.waitForFunction(()=>writingUI.previewFrame?.contentDocument?.querySelector('article table'));
   const p=await page.evaluate(()=>{const d=writingUI.previewFrame.contentDocument;return {toc:[...d.querySelectorAll('.toc a')].map(a=>a.textContent),tasks:d.querySelectorAll('li.task input').length,cells:[...d.querySelectorAll('figure.content-block td')].map(t=>t.textContent),missing:d.querySelector('a.wikilink.missing')?.textContent,fn:!!d.querySelector('.footnotes')}});
   assert.deepEqual(p.toc,['Preview test','Lists','Blocks']);assert.equal(p.tasks,3,'two here, one from the embedded note');assert.deepEqual(p.cells,['2','4','3','9']);assert.equal(p.missing,'nowhere');assert.ok(p.fn);
   const [dl]=await Promise.all([page.waitForEvent('download'),page.evaluate(()=>writeExportItems(writingUI.active).find(i=>i.label==='Word (.docx)').run())]);
   const buf=require('node:fs').readFileSync(await dl.path());assert.equal(buf.slice(0,2).toString(),'PK');
   const name='word/document.xml',at=buf.indexOf(name);const size=buf.readUInt32LE(at-30+18);const xml=buf.slice(at+name.length,at+name.length+size).toString();
   assert.ok(xml.includes('Preview test')&&xml.includes('w:tbl')&&xml.includes('Heading2'),'the .docx carries the text');
   assert.deepEqual(errors,[]);await ctx.close();}
  // ---- phone ----
  if(process.env.STEPS)console.error('phone');
  for(const w of [320,390]){const {ctx,page,errors}=await open('/#/write/essays%2Fon%20writing.md',w);const at='phone '+w;
   const g=await page.evaluate(()=>{const head=document.querySelector('#writeView .write-head');const vis=[...head.querySelectorAll('button,.write-path')].filter(e=>e.getClientRects().length&&getComputedStyle(e).display!=='none');
     return {items:vis.map(e=>e.classList.contains('write-library-toggle')?'back':e.classList.contains('write-path')?'name':e.classList.contains('write-aa')?'Aa':e.classList.contains('write-preview-toggle')?'preview':e.textContent),tabs:!!document.querySelector('.write-tabs')?.getClientRects().length,bar:!!document.getElementById('crumbBar')?.getClientRects().length,overflow:document.documentElement.scrollWidth>innerWidth,name:document.querySelector('.write-path-name').textContent}});
   assert.deepEqual(g.items,['back','name','Aa','preview','•••'],at+': head '+g.items);assert.equal(g.name,'on writing');
   assert.equal(g.tabs,false,at+': no tabs');assert.equal(g.bar,false,at+': the app bar folds');assert.equal(g.overflow,false,at+': sideways pan');
   await page.locator('.cm-line').nth(2).tap();await page.waitForSelector('.write-keybar',{state:'visible'});
   await page.locator('.write-key',{hasText:'#'}).tap();assert.ok((await page.evaluate(()=>writingUI.active.editor.text().split('\n')[2])).startsWith('# '),at+': the # key heads the line');
   await page.locator('.write-library-toggle').tap();await page.waitForFunction(()=>location.hash==='#/write'&&document.getElementById('writeView').dataset.screen==='library');
   assert.equal(await page.evaluate(()=>document.querySelector('#writeView > .write-main').getClientRects().length),0,at+': the library is its own screen');
   assert.deepEqual(errors,[],at);await ctx.close();}
  console.log('PASS: Writing after iA — hanging marks, wrap indent, focus, measure/size, stats, link create; library organizer/search/favorites; quick search outline + commands; style/syntax/authorship; preview TOC/blocks + .docx; phone head, keyboard bar, library screen.');
 }finally{await browser.close();stub.server.close();}
})().catch(e=>{console.error(e);process.exit(1)});
