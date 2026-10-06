// WRITING — the editing tools (owner, 2026-10-06, after iA Writer): Style
// Check strikes through fillers, redundancies and clichés (on the device, no
// model; your own rules and exceptions live in the library document);
// Syntax Highlight colours parts of speech (compromise, loaded on first use);
// Authorship marks what you did not type: pasted text, and Alfred's words
// when you paste one of his replies. Typing over marked text makes it yours.
// None of it changes the file: the marks are the editor's alone, authorship
// sits beside the note on the server (/api/writing/authorship).

// ---- Style Check ----
const writeStyleLists = {
  filler: ['actually','basically','really','very','just','quite','rather','somewhat','totally','literally','simply','pretty much','sort of','kind of','a bit','a little bit','honestly','seriously','definitely','certainly','obviously','clearly','essentially','generally','practically','virtually','absolutely','completely','entirely','extremely','incredibly','truly','utterly','highly','fairly','mostly','perhaps','I think','I believe','I feel','in my opinion','needless to say','it goes without saying',"for what it's worth",'as a matter of fact','the fact that','all things considered','so to speak','more or less','by the way','anyway','of course','in fact','indeed','at the end of the day','in terms of','for all intents and purposes','to be honest','to be fair','you know','I mean'],
  redundancy: ['absolutely essential','added bonus','advance planning','advance warning','all-time record','basic fundamentals','brief summary','close proximity','collaborate together','combine together','completely destroyed','completely eliminate','consensus of opinion','continue on','cooperate together','current trend','end result','enter into','exact same','fall down','final outcome','first began','foreign imports','free gift','future plans','gather together','general public','join together','joint collaboration','past history','past experience','personal opinion','plan ahead','plan in advance','possibly might','postpone until later','protest against','reason why','repeat again','return back','revert back','rise up','same identical','separate out','sum total','true facts','unexpected surprise','unintended mistake','usual custom','very unique','whether or not','written down','ATM machine','PIN number','each and every','first and foremost','null and void','various different','merge together','mix together','new innovation','new invention','over exaggerate','period of time','few in number','large in size','small in size','round in shape','still remains','surrounded on all sides','ask a question','cancel out','circle around','climb up','descend down','ascend up','empty space','face up to','fellow colleague','final conclusion','follow after','harmful injuries','lift up','major breakthrough','meet together','mutual cooperation','open up','outside of','pick and choose','refer back','regular routine','sudden impulse','ultimate goal','unconfirmed rumor','unite together','warn in advance','at this point in time','at the present time','due to the fact that','in spite of the fact that','in the event that','for the purpose of','in order to','a total of','advance reservations','armed gunman','blend together','both alike','cash money','completely full','definite decision','depreciate in value','difficult dilemma','direct confrontation','during the course of','earlier in time','end product','exactly the same','fuse together','grow larger','hollow tube','integrate together','kneel down','link together','may possibly','most unique','nape of the neck','pair of twins','pause for a moment','preplan','reduce down','reply back','shrug your shoulders','spell out in detail','start off','suddenly exploded','terrible tragedy','totally obvious','true fact','visible to the eye'],
  cliche: ['against all odds','avoid like the plague','back to square one','beat around the bush','best thing since sliced bread','better late than never','bite the bullet','brass tacks','by the same token','calm before the storm','clear as mud','cold feet','cut to the chase','dead as a doornail',"devil's advocate",'easier said than done','every cloud has a silver lining','few and far between','fit as a fiddle','game changer','go the extra mile','hit the ground running','hit the nail on the head','in the nick of time','in this day and age','it is what it is','last but not least','leave no stone unturned','let the cat out of the bag','low-hanging fruit','move the needle','needle in a haystack','nip it in the bud','no-brainer','off the beaten path','on the same page','only time will tell','paradigm shift','piece of cake','play it by ear','push the envelope','pushing the envelope','read between the lines','think outside the box','tip of the iceberg','touch base','under the weather','when all is said and done','win-win','without further ado','all walks of life','as luck would have it','at the drop of a hat','ballpark figure','circle back','deep dive','double-edged sword','elephant in the room','fall through the cracks','food for thought','from the ground up','the whole nine yards','heart of gold','in a nutshell','level playing field','light at the end of the tunnel','moment of truth','perfect storm','raise the bar','sea change','shift gears','take it to the next level','time is of the essence','up in the air','writing on the wall','actions speak louder than words','a dime a dozen','around the clock','ahead of the curve','bang for your buck','best practices','cutting edge','state of the art','thought leader','at first glance','par for the course','the bottom line','scratch the surface','tried and true','when push comes to shove','worth its weight in gold','the ball is in your court','a blessing in disguise','break the ice','burn the midnight oil','in the long run','on the fence','once in a blue moon','the last straw']
};
const writeStyleNames={filler:'Filler',redundancy:'Redundancy',cliche:'Cliché',custom:'Your rule'};
// case- and accent-insensitive, one code unit for one: offsets stay the text's
function writeFold(text){let out='';for(const c of text){if(c.length>1){out+=c;continue}const base=c.normalize('NFD')[0],low=base.toLowerCase();out+=low.length===1?low:base}return out}
function writePhrasePattern(phrase){return writeFold(phrase).replace(/[.*+?^${}()|[\]\\]/g,'\\$&').replace(/\s+/g,'\\s+').replace(/['’]/g,"['’]")}
function writeRulePattern(rule){const m=/^\/(.+)\/$/.exec(rule.trim());if(m){try{new RegExp(m[1],'u');return m[1]}catch(e){return null}}return rule.trim()?'(?<![\\p{L}\\p{N}])'+writePhrasePattern(rule)+'(?![\\p{L}\\p{N}])':null}
let writeStyleCache=null;
function writeStyleMatchers(){
  const style=(typeof writeLib!=='undefined'&&writeLib.doc.style)||{},kinds=writePrefs.styleKinds||{},key=JSON.stringify([style,kinds]);
  if(writeStyleCache?.key===key)return writeStyleCache;
  const groups=[];
  for(const kind of ['filler','redundancy','cliche']){if(kinds[kind]===false)continue;const alts=[...writeStyleLists[kind]].sort((a,b)=>b.length-a.length).map(writePhrasePattern);groups.push({kind,re:new RegExp('(?<![\\p{L}\\p{N}])(?:'+alts.join('|')+')(?![\\p{L}\\p{N}])','gu')})}
  const custom=(style.rules||[]).map(writeRulePattern).filter(Boolean);if(custom.length)groups.unshift({kind:'custom',re:new RegExp(custom.map(p=>'(?:'+p+')').join('|'),'giu')});
  const except=(style.exceptions||[]).map(writeRulePattern).filter(Boolean);
  writeStyleCache={key,groups,except:except.length?new RegExp(except.map(p=>'(?:'+p+')').join('|'),'giu'):null};
  return writeStyleCache;
}
// what never takes a mark: frontmatter, fenced and inline code, links' targets
function writeSkipRanges(text){
  const out=[];const fm=/^---\n[\s\S]*?\n(?:---|\.\.\.)(?:\n|$)/.exec(text);if(fm)out.push([0,fm[0].length]);
  for(const m of text.matchAll(/^(```|~~~)[^\n]*\n[\s\S]*?(?:^\1[^\n]*$|(?![\s\S]))/gm))out.push([m.index,m.index+m[0].length]);
  for(const m of text.matchAll(/`[^`\n]+`|\]\([^)\n]*\)|\[\[[^\]\n]*\]\]|https?:\/\/\S+/g))out.push([m.index,m.index+m[0].length]);
  return out;
}
const writeOverlaps=(skip,from,to)=>skip.some(([a,b])=>from<b&&to>a);
function writeStyleRanges(text){
  const folded=writeFold(text),{groups,except}=writeStyleMatchers(),skip=writeSkipRanges(text);
  if(except)for(const m of folded.matchAll(except))if(m[0])skip.push([m.index,m.index+m[0].length]);
  const out=[],taken=[];
  for(const g of groups)for(const m of folded.matchAll(g.re)){if(!m[0])continue;const from=m.index,to=from+m[0].length;if(writeOverlaps(skip,from,to)||writeOverlaps(taken,from,to))continue;taken.push([from,to]);out.push({from,to,class:'write-style write-style-'+g.kind,title:writeStyleNames[g.kind]})}
  return out;
}

// ---- Syntax Highlight ----
let writePOSLoad=null;
function writeLoadScript(src){return new Promise((resolve,reject)=>{const s=document.createElement('script');s.src=src;s.onload=resolve;s.onerror=()=>reject(new Error('Could not load '+src));document.head.append(s)})}
function writePOSReady(){return writePOSLoad||=writeLoadScript('vendor/writing-pos.js').then(()=>window.ManifestPOS)}
const writePOSCache=new Map();
function writePOSRanges(d){
  const view=d.editor.view,state=view.state,doc=state.doc,kinds=writePrefs.posKinds||{},skip=writeSkipRanges(doc.toString()),out=[];
  const seen=new Set();
  const tagged=(from,to)=>{
    if(seen.has(from))return;seen.add(from);
    const text=doc.sliceString(from,to);let tags=writePOSCache.get(text);
    if(!tags){tags=window.ManifestPOS.tag(text);writePOSCache.set(text,tags);if(writePOSCache.size>800)writePOSCache.delete(writePOSCache.keys().next().value)}
    for(const t of tags){const a=from+t.from,b=from+t.to;if(kinds[t.pos]===false||writeOverlaps(skip,a,b))continue;out.push({from:a,to:b,class:'write-pos-'+t.pos})}
  };
  // a paragraph at a time (the tagger reads whole sentences), from the start
  // of the paragraph each visible range begins in
  for(const r of view.visibleRanges){
    let line=doc.lineAt(r.from);
    while(line.text.trim()&&line.number>1&&doc.line(line.number-1).text.trim())line=doc.line(line.number-1);
    while(line.from<=r.to){
      if(line.text.trim()){let last=line;while(last.number<doc.lines&&doc.line(last.number+1).text.trim())last=doc.line(last.number+1);tagged(line.from,last.to);line=last}
      if(line.number>=doc.lines)break;line=doc.line(line.number+1);
    }
  }
  return out;
}

// ---- the layers, recomputed after an edit or a scroll ----
function writeLayers(d,now){
  clearTimeout(d.layerTimer);
  const run=async()=>{
    if(d.closed)return;
    d.editor.setLayer('style',writePrefs.style?writeStyleRanges(d.editor.text()):[]);
    if(writePrefs.syntax){try{await writePOSReady();if(!d.closed&&writePrefs.syntax)d.editor.setLayer('pos',writePOSRanges(d))}catch(e){showToast(e.message,null,'error')}}
    else d.editor.setLayer('pos',[]);
  };
  if(now)run();else d.layerTimer=setTimeout(run,300);
}
writeOnEdit.push(d=>{if(writePrefs.style||writePrefs.syntax)writeLayers(d)});
writeOnMount.push(d=>{
  if(d.layerScroll)return;d.layerScroll=true;
  d.editor.view.scrollDOM.addEventListener('scroll',debounce(()=>{if(writePrefs.syntax&&writingUI.active===d)writeLayers(d,true)},150),{passive:true});
});

// ---- your own rules and exceptions ----
async function writeStyleSettings(){
  if(typeof writeLibLoad==='function')await writeLibLoad();
  const style=(typeof writeLib!=='undefined'&&writeLib.doc.style)||{};
  const root=el('div','cmdbar'),back=el('div','cmdbar-backdrop'),card=el('div','cmdbar-card write-style-settings');
  card.setAttribute('role','dialog');card.setAttribute('aria-modal','true');card.setAttribute('aria-label','Style check');
  const kinds=writePrefs.styleKinds||{};
  const head=el('div','path-picker-heading');head.append(el('span','micro-label','Style check'),pill('close',()=>finish(false)));card.append(head);
  const boxes={};for(const kind of ['filler','redundancy','cliche']){const label=el('label','write-style-kind');const box=document.createElement('input');box.type='checkbox';box.checked=kinds[kind]!==false;boxes[kind]=box;label.append(box,document.createTextNode(' '+writeStyleNames[kind]+'s'));card.append(label)}
  const field=(name,hint,value)=>{const label=el('label','write-style-field');label.append(el('span','micro-label',name),el('span','write-style-hint',hint));const area=document.createElement('textarea');area.className='write-question';area.rows=4;area.value=(value||[]).join('\n');label.append(area);card.append(label);return area};
  const rules=field('Your rules','One word or phrase a line; /regex/ for a pattern. Matched without regard to case or accents.',style.rules);
  const exceptions=field('Exceptions','Words or phrases never to mark, even when a list or a rule has them.',style.exceptions);
  const actions=el('div','write-actions');actions.append(pillLight('save',()=>finish(true)));card.append(actions);
  root.append(back,card);document.body.append(root);const release=containDialogFocus(root,rules);
  back.onclick=()=>finish(false);card.onkeydown=e=>{if(e.key==='Escape'){e.preventDefault();e.stopPropagation();finish(false)}};
  async function finish(save){
    if(save){
      const lines=a=>a.value.split('\n').map(s=>s.trim()).filter(Boolean);
      const bad=lines(rules).concat(lines(exceptions)).find(r=>/^\/.+\/$/.test(r)&&!writeRulePattern(r));if(bad){showToast('This pattern is not valid: '+bad,null,'error');return}
      writePrefs.styleKinds=Object.fromEntries(Object.entries(boxes).map(([k,b])=>[k,b.checked]));
      const r=lines(rules),x=lines(exceptions);
      if(typeof writeLibChange==='function'&&!await writeLibChange(doc=>{doc.style={rules:r,exceptions:x}}))return;
      writeSetPref('style',true);
    }
    release();root.remove();writingUI.active?.editor.focus();
  }
}

// ---- Authorship ----
// re-find each stored passage in today's text: the exact span when the note
// is unchanged since, else its quote with its surroundings, else the quote
function writeReanchor(text,doc,revision){
  if(doc.revision&&doc.revision===revision)return doc.ranges.filter(r=>r.to<=text.length&&text.slice(r.from,r.to)===r.quote);
  const out=[];
  for(const r of doc.ranges||[]){
    if(!r.quote)continue;let at=-1;const ctx=(r.prefix||'')+r.quote+(r.suffix||''),pos=text.indexOf(ctx);
    if(pos>=0&&text.indexOf(ctx,pos+1)<0)at=pos+(r.prefix||'').length;
    else{const q=text.indexOf(r.quote);if(q>=0&&text.indexOf(r.quote,q+1)<0)at=q}
    if(at>=0)out.push({from:at,to:at+r.quote.length,author:r.author});
  }
  return out;
}
async function writeLoadAuthors(d){
  try{const doc=await writeFetch('/api/writing/authorship?path='+encodeURIComponent(d.path));if(d.closed)return;
    const ranges=writeReanchor(d.editor.text(),doc,d.revision);d.editor.setAuthors(ranges);d.authorsSaved=JSON.stringify(d.editor.authors().map(r=>[r.from,r.to,r.author]));d.authorsLoaded=true;
  }catch(e){d.authorsLoaded=true}
}
// stored against the saved bytes, so only when the editor matches them
function writePersistAuthors(d){
  clearTimeout(d.authorTimer);
  d.authorTimer=setTimeout(async()=>{
    if(d.closed||!d.authorsLoaded||d.readOnly||writeDirty(d))return;
    const ranges=d.editor.authors(),sig=JSON.stringify(ranges.map(r=>[r.from,r.to,r.author]));if(sig===d.authorsSaved)return;
    try{await writeFetch('/api/writing/authorship?path='+encodeURIComponent(d.path),{revision:d.revision,ranges,authors:[{id:'alfred',name:'Alfred',kind:'ai'},{id:'pasted',name:'Pasted',kind:'human'}]},'PUT');d.authorsSaved=sig}catch(e){}
  },600);
}
writeOnEdit.push(writePersistAuthors);writeOnSave.push(writePersistAuthors);
// a paste: your own words copied in this editor stay yours; one of Alfred's
// replies is his; anything else is "pasted" (the editor's default)
let writeCopied=null;
document.addEventListener('copy',e=>writeNoteCopy(e),true);document.addEventListener('cut',e=>writeNoteCopy(e),true);
function writeNoteCopy(e){
  const d=writingUI.active;if(!d||!e.target.closest?.('.write-editor'))return;
  const sel=d.editor.selection();if(sel.empty)return;
  const marked=d.editor.authors().filter(r=>r.from<sel.to&&r.to>sel.from);
  const covered=marked.reduce((n,r)=>n+Math.min(r.to,sel.to)-Math.max(r.from,sel.from),0);
  writeCopied={text:d.editor.text().slice(sel.from,sel.to),author:covered*2>sel.to-sel.from?marked[0].author:'self'};
}
function writePasteAuthor(d,text){
  const norm=s=>s.replace(/\s+/g,' ').trim();
  if(writeCopied&&norm(writeCopied.text)===norm(text))return writeCopied.author;
  const t=norm(text);if(t.length>=12)for(const th of d.comments?.threads||[])for(const reply of th.replies||[])if(reply.author==='alfred'&&norm(reply.body).includes(t))return 'alfred';
  return null;
}
function writeAuthorSummary(d){
  const text=d.editor.text(),total=text.replace(/\s/g,'').length;if(!total)return '';
  const by={};for(const r of d.editor.authors())by[r.author]=(by[r.author]||0)+text.slice(r.from,r.to).replace(/\s/g,'').length;
  const pct=n=>Math.round(n/total*100);const others=Object.values(by).reduce((a,b)=>a+b,0);
  return [pct(total-others)+'% you',...Object.entries(by).map(([a,n])=>pct(n)+'% '+(a==='alfred'?'Alfred':a==='pasted'?'pasted':a))].join(' · ');
}
writeMenuHooks.push(d=>{
  const out=[{header:'Authorship · '+(writeAuthorSummary(d)||'empty')}],sel=d.editor.selection();
  if(!sel.empty&&!d.readOnly)out.push({label:'mark selection as yours',run:()=>{d.editor.markAuthor(sel.from,sel.to,null);writePersistAuthors(d)}},{label:'mark selection as Alfred’s',run:()=>{d.editor.markAuthor(sel.from,sel.to,'alfred');writePersistAuthors(d)}},{label:'mark selection as pasted',run:()=>{d.editor.markAuthor(sel.from,sel.to,'pasted');writePersistAuthors(d)}});
  out.push({label:(writePrefs.authors?'hide':'show')+' authors',run:()=>writeToggle('authors')},{label:'style check settings…',run:()=>writeStyleSettings()});
  return out;
});
