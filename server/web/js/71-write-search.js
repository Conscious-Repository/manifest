// WRITING — Quick Search (owner, 2026-10-06, after iA Writer 8): one box for
// the open document's outline, files by name, the vault's text and every
// command. ⌘O opens it; ⇧⌘P (or a leading ">") lists only commands, each
// with its shortcut. It is the shared command overlay (.cmdbar), not a new
// modal.

// the open document's headings, outside fenced code
function writeOutline(text){
  const out=[];let fence=false,pos=0;
  for(const line of text.split('\n')){
    if(/^\s*(```|~~~)/.test(line))fence=!fence;
    const m=!fence&&/^(#{1,6})\s+(.+?)\s*#*\s*$/.exec(line);
    if(m)out.push({level:m[1].length,title:m[2],from:pos});
    pos+=line.length+1;
  }
  return out;
}
// every command Writing offers, with what it does now
function writeCommands(){
  const d=writingUI.active,p=writePrefs,on=v=>v?' · on':'';
  const list=[
    {label:'Focus: sentence',key:'focus',run:()=>writeSetFocus('sentence')},
    {label:'Focus: paragraph',run:()=>writeSetFocus('paragraph')},
    {label:'Focus: off',run:()=>writeSetFocus('off')},
    {label:'Typewriter scrolling'+on(p.typewriter),run:()=>writeSetPref('typewriter',!p.typewriter)},
    {label:'Syntax highlight'+on(p.syntax),key:'syntax',run:()=>writeToggle('syntax')},
    {label:'Style check'+on(p.style),key:'style',run:()=>writeToggle('style')},
    {label:'Show authors'+on(p.authors),key:'authors',run:()=>writeToggle('authors')},
    {label:'Library'+on(p.library),key:'library',run:()=>writeToggleLibrary()},
    {label:'New file',run:()=>writeCreate(typeof writeLibFolder==='function'?writeLibFolder():'')},
    {label:'New folder',run:()=>typeof writeCreateFolder==='function'&&writeCreateFolder(typeof writeLibFolder==='function'?writeLibFolder():'')},
    {label:'Line length: 64',run:()=>writeSetPref('measure',64)},{label:'Line length: 72',run:()=>writeSetPref('measure',72)},{label:'Line length: 80',run:()=>writeSetPref('measure',80)},
    {label:'Text size: small',run:()=>writeSetPref('size','sm')},{label:'Text size: default',run:()=>writeSetPref('size','md')},{label:'Text size: large',run:()=>writeSetPref('size','lg')},
  ];
  if(typeof writeTogglePreview==='function')list.push({label:'Preview: side by side',key:'preview',run:()=>writeSetPref('preview','split')},{label:'Preview: full',run:()=>writeSetPref('preview','full')},{label:'Preview: off',run:()=>writeSetPref('preview','off')});
  if(d){
    list.push({label:'Find in document',run:()=>d.editor.find()},{label:'Comments',run:()=>writeToggleMargin()},
      {label:'Rename…',run:()=>writeMenuRun('rename')},{label:'Move file to…',run:()=>writeMenuRun('move')},{label:'Export Markdown',run:()=>writeExport(d)},
      {label:(typeof writeIsFavorite==='function'&&writeIsFavorite(d.path)?'Remove from':'Add to')+' favorites',run:()=>writeToggleFavorite(d.path)});
    for(const hook of writeMenuHooks)for(const t of hook(d))if(t.run&&t.label)list.push({label:t.label[0].toUpperCase()+t.label.slice(1),run:t.run});
  }
  return list.map(c=>({...c,shortcut:c.key?writeShortcut(c.key):''}));
}
// the ··· rename / move flows, reached from a command
function writeMenuRun(value){const d=writingUI.active;if(!d)return;writeMenuChoice(d,{value})}

function writeQuickSearch(initial=''){
  if(document.querySelector('.write-quick'))return;
  const root=el('div','cmdbar write-quick'),back=el('div','cmdbar-backdrop'),card=el('div','cmdbar-card');
  card.setAttribute('role','dialog');card.setAttribute('aria-modal','true');card.setAttribute('aria-label','Quick search');
  const input=inputEl('Go to a heading, file or text… (> for commands)');input.className='cmdbar-input';input.value=initial;
  input.setAttribute('role','combobox');input.setAttribute('aria-expanded','true');input.setAttribute('aria-autocomplete','list');
  const list=el('div','path-picker-list write-quick-list');list.id='write-quick-'+Math.random().toString(36).slice(2);list.setAttribute('role','listbox');input.setAttribute('aria-controls',list.id);
  const hint=el('div','path-picker-hint','↑↓ to move · enter to open · > commands · esc to close');
  card.append(input,list,hint);root.append(back,card);document.body.append(root);
  const release=containDialogFocus(root,input);
  let rows=[],selected=0,textHits=null,textQuery='',settled=false;
  const finish=item=>{if(settled)return;settled=true;release();root.remove();if(item)item.run();else writingUI.active?.editor.focus()};
  const runText=debounce(async q=>{
    if(q.length<2||q.startsWith('>')){textHits=null;return}
    try{const r=await writeFetch('/api/writing/search?q='+encodeURIComponent(q)+'&limit=20');if(input.value.trim()===q){textHits=r.results||[];textQuery=q;paint()}}catch(e){textHits=[];paint()}
  },160);
  function paint(){
    const raw=input.value,q=raw.trim(),commandsOnly=q.startsWith('>'),cq=(commandsOnly?q.slice(1):q).trim().toLowerCase();
    const groups=[];
    const cmds=writeCommands().filter(c=>!cq||c.label.toLowerCase().includes(cq));
    if(!commandsOnly){
      const d=writingUI.active;
      if(d){const heads=writeOutline(d.editor.text()).filter(h=>!cq||h.title.toLowerCase().includes(cq));
        if(heads.length)groups.push({name:'In this document',items:heads.slice(0,40).map(h=>({label:'#'.repeat(h.level)+' '+h.title,indent:h.level-1,run:()=>{const at=h.from+h.level+1;d.editor.locate(at,at)}}))})}
      const files=writingUI.files.map(f=>({f,score:cq?fuzzyScore(cq,f.name.toLowerCase()):1})).filter(x=>x.score>=0)
        .sort((a,b)=>b.score-a.score||b.f.modified-a.f.modified).slice(0,cq?12:8);
      if(files.length)groups.push({name:cq?'Files':'Recent files',items:files.map(({f})=>({label:f.name,detail:f.path.includes('/')?f.path.replace(/\/[^/]*$/,''):'',run:()=>writeNavigate(f.path)}))});
      if(textHits&&textQuery===q){const named=new Set(files.map(x=>x.f.path));const hits=textHits.filter(h=>!named.has(h.path));if(hits.length)groups.push({name:'Text',items:hits.map(h=>({label:h.name,detail:h.snippet||'',run:()=>writeNavigate(h.path)}))})}
      if(cq&&cmds.length)groups.push({name:'Commands',items:cmds.slice(0,4)});
    }else groups.push({name:'Commands',items:cmds});
    rows=[];list.replaceChildren();
    for(const g of groups){list.append(el('div','write-quick-group micro-label',g.name));
      for(const item of g.items){const i=rows.length;rows.push(item);
        const row=el('div','path-picker-row write-quick-row'+(i===selected?' on':''));row.id=list.id+'-'+i;row.setAttribute('role','option');row.setAttribute('aria-selected',String(i===selected));
        if(item.indent)row.style.paddingLeft='calc(var(--sp-7) + '+item.indent+'em)';
        row.append(el('span','write-quick-label',item.label));if(item.detail)row.append(el('span','path-picker-detail',item.detail));if(item.shortcut)row.append(el('span','action-menu-shortcut',item.shortcut));
        row.onmousedown=e=>e.preventDefault();row.onclick=()=>finish(item);list.append(row)}}
    if(!rows.length)list.append(el('div','path-picker-hint',commandsOnly?'no matching commands':'nothing matches'));
    selected=Math.max(0,Math.min(selected,rows.length-1));
    input.setAttribute('aria-activedescendant',rows.length?list.id+'-'+selected:'');
    list.querySelector('#'+CSS.escape(list.id+'-'+selected))?.scrollIntoView({block:'nearest'});
  }
  input.oninput=()=>{selected=0;paint();runText(input.value.trim())};
  card.onkeydown=e=>{
    if(e.key==='Escape'){e.preventDefault();e.stopPropagation();finish(null)}
    else if(['ArrowDown','ArrowUp','Enter'].includes(e.key)&&e.target===input){
      e.preventDefault();e.stopPropagation();
      if(e.key==='Enter'){if(rows[selected])finish(rows[selected]);return}
      selected=(selected+(e.key==='ArrowDown'?1:-1)+rows.length)%Math.max(1,rows.length);
      list.querySelectorAll('.write-quick-row').forEach((r,i)=>{r.classList.toggle('on',i===selected);r.setAttribute('aria-selected',String(i===selected))});
      input.setAttribute('aria-activedescendant',list.id+'-'+selected);list.querySelector('#'+CSS.escape(list.id+'-'+selected))?.scrollIntoView({block:'nearest'});
    }
  };
  back.onclick=()=>finish(null);paint();runText(input.value.trim());
  input.setSelectionRange(input.value.length,input.value.length);
}
