// The rail's state dot must sit with its title, not float in the gap. On
// 2026-09-26 (1440px, deployed 6161a10) the conversation-list dots drifted far
// right of short titles: .chat-rail-title was flex: 1 1 auto, so its box ate
// the whole line and the dot landed at the row's right end, beside ⋯ — and a
// ⟳ (resumable) after it moved that column again row by row.
// Every row: the dot follows the title's text at one fixed gap, the ⋯ menu
// still ends the line at one right edge, a long title ellipsises with the dot
// still visible inside the row, and nothing overflows the row — at desktop and
// phone widths.
const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
(async()=>{const browser=await chromium.launch({headless:true});try{
 const root=path.join(__dirname,'../web'),css=fs.readdirSync(path.join(root,'css')).filter(f=>f.endsWith('.css')&&f!=='99-local-fonts.css').sort();
 const page=await browser.newPage();await page.route('**/*',r=>r.abort());
 await page.setContent('<section class="chat-page" id="chatView"><div class="chat-shell mf-chat-nav-open"><aside class="chat-rail" id="chatRail"><div class="chat-inbox-rows" id="chatInboxRows"></div></aside><div class="chat-main"></div></div></section>');
 for(const f of css)await page.addStyleTag({content:fs.readFileSync(path.join(root,'css',f),'utf8')});
 // [title, agent state, resumable] — the chatTermRow anatomy after the inbox
 // decoration: title · state dot · ⟳? · (✎ moved into ⋯) · ⋯
 const rows=[
  ['claude','working',false],
  ['Sequence planning','idle',true],
  ['manifest','blocked',false],
  ['A much longer conversation title that cannot fit on one rail line at any width','unknown',true],
 ];
 let failures=[];
 for(const width of [1440,1024,861,412,390,320]){
  await page.setViewportSize({width,height:900});
  const out=await page.evaluate(rows=>{
   const host=document.getElementById('chatInboxRows');host.replaceChildren();
   const mk=(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls;if(text!==undefined)e.textContent=text;return e;};
   for(const [name,state,resumable] of rows){
    const row=mk('div','chat-rail-row'),top=mk('div','chat-rail-top');
    top.append(mk('span','chat-rail-title',name));
    const dot=mk('span','status-dot terminal-state-dot'+(state==='working'?' on':''));dot.dataset.agentState=state;top.append(dot);
    if(resumable)top.append(mk('span','chat-rail-resume','⟳'));
    const menu=mk('details','chat-row-menu');menu.append(mk('summary','','⋯'));top.append(menu);
    const meta=mk('div','chat-rail-meta');meta.append(mk('span','chat-rail-spirit','manifest'),mk('span','chat-rail-when','Sep 12'));
    row.append(top,meta);host.append(row);
   }
   const res=[],gaps=[],menuRights=[];
   for(const row of host.querySelectorAll('.chat-rail-row')){
    const t=row.querySelector('.chat-rail-title'),d=row.querySelector('.terminal-state-dot'),m=row.querySelector('.chat-row-menu'),top=row.querySelector('.chat-rail-top');
    const tr=t.getBoundingClientRect(),dr=d.getBoundingClientRect(),mr=m.getBoundingClientRect(),rr=top.getBoundingClientRect();
    // where the title's TEXT ends (its box may be wider than its words)
    const range=document.createRange();range.selectNodeContents(t);const words=range.getBoundingClientRect();
    const textRight=Math.min(words.right,tr.right),name=JSON.stringify(t.textContent.slice(0,24));
    gaps.push(Math.round(dr.left-textRight));menuRights.push(Math.round(mr.right));
    if(dr.left-textRight>16)res.push(`${name}: dot is ${Math.round(dr.left-textRight)}px right of the title text (floats in the gap)`);
    if(dr.left<textRight-0.5)res.push(`${name}: dot overlaps the title`);
    if(dr.width<4||dr.right>mr.left+0.5)res.push(`${name}: dot hidden or under the ⋯ menu`);
    if(mr.right>rr.right+0.5||top.scrollWidth>top.clientWidth+0.5)res.push(`${name}: the title line overflows the row`);
    if(Math.abs((dr.top+dr.bottom)/2-(tr.top+tr.bottom)/2)>3)res.push(`${name}: dot is not centred on the title line`);
    if(t.scrollWidth>t.clientWidth+0.5&&getComputedStyle(t).textOverflow!=='ellipsis')res.push(`${name}: long title cut without an ellipsis`);
   }
   if(new Set(gaps).size>1)res.push('title→dot gap varies by row: '+gaps.join('/'));
   if(new Set(menuRights).size>1)res.push('⋯ menus do not end on one edge: '+menuRights.join('/'));
   return res;
  },rows);
  failures.push(...out.map(f=>width+'px: '+f));
 }
 assert.deepEqual(failures,[],'rail state dots misaligned:\n'+failures.join('\n'));
 console.log('PASS rail state dots follow their titles at one gap, ⋯ ends every line on one edge, long titles ellipsise with the dot visible, at 1440/1024/861/412/390/320px.');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exit(1)});
