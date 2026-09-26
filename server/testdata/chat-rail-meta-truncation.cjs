// The rail's metadata line must never cut a token mid-word. 2026-09-26 the
// rail read "Run finished · Claude Coc" and "Codex Seq": the meta line is a
// nowrap flex row with overflow hidden, so the last item was clipped at the
// rail edge with no ellipsis (text-overflow does nothing on a flex container).
// Every meta item must be either whole on the line or dropped whole; only an
// item that alone is wider than the line may shorten, and then with an ellipsis.
const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
(async()=>{const browser=await chromium.launch({headless:true});try{
 const root=path.join(__dirname,'../web'),css=fs.readdirSync(path.join(root,'css')).filter(f=>f.endsWith('.css')&&f!=='99-local-fonts.css').sort();
 const page=await browser.newPage();await page.route('**/*',r=>r.abort());
 await page.setContent('<section class="chat-page" id="chatView"><div class="chat-shell mf-chat-nav-open"><aside class="chat-rail" id="chatRail"><div class="chat-inbox-rows" id="chatInboxRows"></div></aside><div class="chat-main"></div></div></section>');
 for(const f of css)await page.addStyleTag({content:fs.readFileSync(path.join(root,'css',f),'utf8')});
 const rows=[
  ['Run finished','Claude Code','manifest','Sep 12'],
  ['Run finished','Codex','Sequence planning','Sep 11'],
  ['1 ready for review · Finished','Alfred','Sep 12'],
  ['1 need revision · Run finished · input pending','Kairos · team','Sep 10'],
  ['Queued','Codex','a-very-long-working-folder-name-without-spaces','today'],
 ];
 let failures=[];
 for(const width of [1440,1024,861,412,390,320]){
  await page.setViewportSize({width,height:900});
  const cuts=await page.evaluate(rows=>{
   const host=document.getElementById('chatInboxRows');host.replaceChildren();
   for(const parts of rows){const row=document.createElement('div');row.className='chat-rail-row';
    const top=document.createElement('div');top.className='chat-rail-top';const t=document.createElement('span');t.className='chat-rail-title';t.textContent='A conversation';top.append(t);
    const meta=document.createElement('div');meta.className='chat-rail-meta';
    const cls=['chat-row-state','chat-inbox-agent','chat-rail-spirit','chat-rail-when'];
    parts.forEach((p,i)=>{const s=document.createElement('span');s.className=cls[i===parts.length-1?3:i];s.textContent=p;meta.append(s);});
    row.append(top,meta);host.append(row);}
   const out=[];
   for(const meta of host.querySelectorAll('.chat-rail-meta')){const m=meta.getBoundingClientRect();
    [...meta.children].forEach((c,i)=>{const r=c.getBoundingClientRect(),cs=getComputedStyle(c);
     const onLine=r.top<m.bottom-1&&r.bottom>m.top+1;
     if(!onLine)return; // dropped whole: fine
     const clippedByLine=r.right>m.right+0.5;
     const shortened=c.scrollWidth>c.clientWidth+0.5;
     if(clippedByLine)out.push(`"${c.textContent}" clipped at the line edge (${Math.round(r.right)}>${Math.round(m.right)})`);
     else if(shortened&&cs.textOverflow!=='ellipsis')out.push(`"${c.textContent}" shortened without an ellipsis`);
     else if(shortened&&i>0)out.push(`"${c.textContent}" shortened although an earlier token could drop`);
    });
    const first=meta.children[0].getBoundingClientRect();
    if(!(first.top<m.bottom-1))out.push('the run state itself was dropped');
    if(m.height>parseFloat(getComputedStyle(meta).lineHeight)*1.6)out.push('meta line grew past one line ('+m.height+'px)');
   }
   return out;
  },rows);
  failures.push(...cuts.map(c=>width+'px: '+c));
 }
 assert.deepEqual(failures,[],'rail metadata cut mid-token:\n'+failures.join('\n'));
 console.log('PASS rail metadata keeps whole tokens (dropped whole or ellipsised when alone) at 1440/1024/861/412/390/320px.');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exit(1)});
