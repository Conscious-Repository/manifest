import {EditorState, StateEffect, StateField, Compartment, Transaction, Annotation} from '@codemirror/state';
import {EditorView, Decoration, keymap} from '@codemirror/view';
import {defaultKeymap, history, historyKeymap, indentWithTab, undo, redo} from '@codemirror/commands';
import {markdown, markdownKeymap, markdownLanguage} from '@codemirror/lang-markdown';
import {syntaxHighlighting, HighlightStyle, syntaxTree, bracketMatching} from '@codemirror/language';
import {tags} from '@lezer/highlight';
import {autocompletion, completionKeymap, startCompletion} from '@codemirror/autocomplete';
import {searchKeymap, highlightSelectionMatches, openSearchPanel} from '@codemirror/search';

// ---- comment anchors (set from the margin) ----
const setAnchors = StateEffect.define();
const anchors = StateField.define({
 create: () => Decoration.none,
 update(value, tr) {
  value = value.map(tr.changes);
  for (const effect of tr.effects) if (effect.is(setAnchors)) {
   value = Decoration.set(effect.value.filter(a=>a.from>=0&&a.to<=tr.state.doc.length&&a.from<a.to).map(a=>Decoration.mark({class:'write-anchor',attributes:{'data-thread':a.id}}).range(a.from,a.to)),true);
  }
  return value;
 }, provide: f => EditorView.decorations.from(f)
});
const style = HighlightStyle.define([
 {tag:tags.heading1,class:'write-h1'}, {tag:tags.heading2,class:'write-h2'},
 {tag:[tags.heading3,tags.heading4,tags.heading5,tags.heading6],class:'write-h3'},
 {tag:tags.strong,fontWeight:'700'},{tag:tags.emphasis,fontStyle:'italic'},
 {tag:tags.strikethrough,textDecoration:'line-through'},
 {tag:[tags.url,tags.link],class:'write-link'}, {tag:tags.monospace,class:'write-code'},
 {tag:tags.meta,class:'write-syntax'}
]);

// ---- Markdown as iA Writer shows it ----
// The syntax is always there and always editable; it is dimmed, never hidden.
// Heading marks hang in the left margin and a list item's wrapped lines align
// with its text (71-write.css sizes both from --n, the mark's length in
// characters). Frontmatter is dimmed as a block.
function metadataEnd(state) {
 const first=state.doc.line(1);if(first.text!=='---')return 0;
 for(let i=2;i<=state.doc.lines;i++){const line=state.doc.line(i);if(line.text==='---'||line.text==='...')return Math.min(line.to+1,state.doc.length)}return 0;
}
const dimmed=new Set(['EmphasisMark','CodeMark','LinkMark','QuoteMark','StrikethroughMark','TaskMarker','CodeInfo','URL','HardBreak']);
const width=text=>[...text].reduce((n,c)=>n+(c==='\t'?4:1),0);
function presentation(state){
 const ranges=[], fmEnd=metadataEnd(state);
 if(fmEnd>0)for(let pos=0;pos<fmEnd;){const line=state.doc.lineAt(pos);ranges.push(Decoration.line({class:'write-frontmatter'}).range(line.from));pos=line.to+1}
 syntaxTree(state).iterate({enter:n=>{
  if(n.from<fmEnd)return;
  if(n.name==='HeaderMark'&&state.doc.lineAt(n.from).from===n.from){
   // ATX heading: "## " hangs; a setext underline is just dimmed
   let end=n.to;if(state.doc.sliceString(end,end+1)===' ')end++;
   if(state.doc.sliceString(n.from,n.to).startsWith('#')){ranges.push(Decoration.line({class:'write-hline',attributes:{style:'--n:'+(end-n.from),'data-level':String(Math.min(3,n.to-n.from))}}).range(n.from));ranges.push(Decoration.mark({class:'write-hmark'}).range(n.from,end))}
   else ranges.push(Decoration.mark({class:'write-mark'}).range(n.from,n.to));
   return;
  }
  if(n.name==='HeaderMark'){ranges.push(Decoration.mark({class:'write-mark'}).range(n.from,n.to));return}
  if(n.name==='ListMark'){
   const line=state.doc.lineAt(n.from);let end=n.to;if(state.doc.sliceString(end,end+1)===' ')end++;
   const task=/^\[[ xX]\] /.exec(state.doc.sliceString(end,end+4));if(task)end+=4;
   const lead=state.doc.sliceString(line.from,n.from);
   if(/^\s*$/.test(lead)){const len=width(state.doc.sliceString(line.from,end));
    ranges.push(Decoration.line({class:'write-li',attributes:{style:'--n:'+len}}).range(line.from));
    ranges.push(Decoration.mark({class:'write-lmark'}).range(line.from,end));}
   return;
  }
  if(n.name==='TaskMarker'){const before=state.doc.sliceString(n.from-3,n.from);if(/[-*+] $|\d[.)] $/.test(before))return}
  if(dimmed.has(n.name)&&n.to>n.from)ranges.push(Decoration.mark({class:'write-mark'}).range(n.from,n.to));
 }});
 const re=/\[\[([^\]\n]+)\]\]/g;
 for(let i=1;i<=state.doc.lines;i++){const line=state.doc.line(i);if(line.from<fmEnd)continue;let match;
  while((match=re.exec(line.text))){const from=line.from+match.index,to=from+match[0].length;
   ranges.push(Decoration.mark({class:'write-wikilink',attributes:{'data-target':match[1].split('|')[0].trim()}}).range(from,to));
  }
 }
 return Decoration.set(ranges,true);
}
const markup=StateField.define({create:presentation,update:(value,tr)=>tr.docChanged||syntaxTree(tr.state)!==syntaxTree(tr.startState)?presentation(tr.state):value,provide:field=>EditorView.decorations.from(field)});

// ---- focus: the sentence or paragraph being written stays bright ----
function paragraphAt(state,pos){
 let first=state.doc.lineAt(pos),last=first;
 if(!first.text.trim())return {from:first.from,to:first.to};
 while(first.number>1){const prev=state.doc.line(first.number-1);if(!prev.text.trim())break;first=prev}
 while(last.number<state.doc.lines){const next=state.doc.line(last.number+1);if(!next.text.trim())break;last=next}
 return {from:first.from,to:last.to};
}
function sentenceAt(state,pos){
 const para=paragraphAt(state,pos),text=state.doc.sliceString(para.from,para.to),at=pos-para.from;
 const ends=/[.!?…]+["'”’)\]]*(?=\s|$)/g;let start=0,match;
 while((match=ends.exec(text))){const end=match.index+match[0].length;if(at<=end)return {from:para.from+start,to:para.from+end};start=end;while(/\s/.test(text[start]||''))start++}
 return {from:para.from+start,to:para.to};
}
function focusDecorations(scope){
 return StateField.define({
  create:state=>dimOutside(state,scope),
  update:(value,tr)=>tr.docChanged||tr.selection?dimOutside(tr.state,scope):value,
  provide:f=>EditorView.decorations.from(f)
 });
}
function dimOutside(state,scope){
 const span=(scope==='sentence'?sentenceAt:paragraphAt)(state,state.selection.main.head),ranges=[];
 if(span.from>0)ranges.push(Decoration.mark({class:'write-dim'}).range(0,span.from));
 if(span.to<state.doc.length)ranges.push(Decoration.mark({class:'write-dim'}).range(span.to,state.doc.length));
 return Decoration.set(ranges);
}
const focusFields={sentence:focusDecorations('sentence'),paragraph:focusDecorations('paragraph')};
// typewriter: the line being written stays in the middle of the window
const typewriter=EditorState.transactionExtender.of(tr=>(tr.docChanged||tr.selection)&&tr.isUserEvent&&(tr.isUserEvent('input')||tr.isUserEvent('delete')||tr.isUserEvent('select')||tr.isUserEvent('move'))?{effects:EditorView.scrollIntoView(tr.newSelection.main.head,{y:'center'})}:null);

// ---- layers: style check, parts of speech (computed outside, mapped here) ----
const setLayer=StateEffect.define();
const layers=StateField.define({
 create:()=>({}),
 update(value,tr){
  let next=value;
  if(tr.docChanged){next={};for(const k in value)next[k]=value[k].map(tr.changes)}
  for(const e of tr.effects)if(e.is(setLayer)){
   next=next===value?{...value}:next;
   const len=tr.state.doc.length;
   next[e.value.name]=Decoration.set(e.value.ranges.filter(r=>r.from>=0&&r.to<=len&&r.from<r.to).map(r=>Decoration.mark({class:r.class,attributes:r.title?{title:r.title}:undefined}).range(r.from,r.to)),true);
  }
  return next;
 },
 provide:f=>EditorView.decorations.compute([f],state=>{const sets=Object.values(state.field(f));if(!sets.length)return Decoration.none;const all=[];for(const s of sets){const it=s.iter();while(it.value){all.push(it.value.range(it.from,it.to));it.next()}}return Decoration.set(all,true)})
});

// ---- authorship: who wrote which characters ----
// Typing is the owner's and is never marked. A paste is marked "pasted" (or
// the author the page names for it, such as Alfred when the text is one of
// his replies); typing inside a marked passage makes those characters yours.
const pasteAuthor=Annotation.define();
const setAuthors=StateEffect.define(), markAuthor=StateEffect.define();
function carve(ranges,from,to){const out=[];for(const r of ranges){if(r.to<=from||r.from>=to){out.push(r);continue}if(r.from<from)out.push({...r,to:from});if(r.to>to)out.push({...r,from:to})}return out}
function tidy(ranges){ranges=ranges.filter(r=>r.to>r.from).sort((a,b)=>a.from-b.from);const out=[];for(const r of ranges){const last=out.at(-1);if(last&&last.author===r.author&&last.to>=r.from)last.to=Math.max(last.to,r.to);else out.push({...r})}return out}
const authorship=StateField.define({
 create:()=>[],
 update(ranges,tr){
  if(tr.docChanged){
   ranges=ranges.map(r=>({...r,from:tr.changes.mapPos(r.from,1),to:tr.changes.mapPos(r.to,-1)}));
   const paste=tr.isUserEvent('input.paste')||tr.isUserEvent('input.drop'),typed=!paste&&tr.isUserEvent('input');
   tr.changes.iterChanges((fA,tA,fB,tB)=>{if(tB<=fB)return;
    if(paste){ranges=carve(ranges,fB,tB);const who=tr.annotation(pasteAuthor)||'pasted';if(who!=='self')ranges.push({from:fB,to:tB,author:who})}
    else if(typed)ranges=carve(ranges,fB,tB);
   });
  }
  for(const e of tr.effects){
   if(e.is(setAuthors))ranges=e.value.map(r=>({from:r.from,to:r.to,author:r.author}));
   if(e.is(markAuthor)){ranges=carve(ranges,e.value.from,e.value.to);if(e.value.author)ranges.push({from:e.value.from,to:e.value.to,author:e.value.author})}
  }
  return tidy(ranges.filter(r=>r.from>=0&&r.to<=tr.state.doc.length));
 }
});
const authorView=EditorView.decorations.compute([authorship],state=>Decoration.set(state.field(authorship).map(r=>Decoration.mark({class:'write-author write-author-'+(r.author==='alfred'?'ai':r.author==='pasted'?'pasted':'other'),attributes:{'data-author':r.author}}).range(r.from,r.to)),true));

window.ManifestEditor = {
 create(parent,options){
  const readOnly=new Compartment(),focus=new Compartment(),writer=new Compartment(),authors=new Compartment();
  const linkTarget=f=>{const name=f.path.replace(/^.*\//,'').replace(/\.md$/i,'');return options.files().filter(o=>o.path.replace(/^.*\//,'').replace(/\.md$/i,'').toLowerCase()===name.toLowerCase()).length>1?f.path.replace(/\.md$/i,''):name};
  const view=new EditorView({parent,state:EditorState.create({doc:options.text,selection:{anchor:(options.text.match(/^---\n[\s\S]*?\n(?:---|\.\.\.)\n/)||[''])[0].length},extensions:[
   history(), markdown({base:markdownLanguage}), bracketMatching(), syntaxHighlighting(style), EditorView.lineWrapping,
   anchors, markup, layers, authorship, highlightSelectionMatches(),
   focus.of([]), writer.of([]), authors.of([]),
   readOnly.of(EditorState.readOnly.of(!!options.readOnly)),
   EditorView.contentAttributes.of({'aria-label':'Document editor',spellcheck:'true'}),
   keymap.of([{key:'Mod-s',run:()=>{options.save();return true}},{key:'Mod-Alt-m',run:()=>{options.comment();return true}},...(options.keys||[]),...markdownKeymap,...defaultKeymap,...historyKeymap,...completionKeymap,...searchKeymap,indentWithTab]),
   autocompletion({override:[ctx=>{
    // [[ links: the shortest name that is unique in the vault, as iA inserts
    const link=ctx.matchBefore(/\[\[[^\]\n]*/);
    if(link)return {from:link.from+2,options:options.files().map(f=>({label:f.name,detail:f.path,type:'text',apply:linkTarget(f)+']]'})),validFor:/[^\]\n]*/};
    // a content block: a path alone on its line, started with /
    const block=ctx.matchBefore(/^\/[^\s]*/);
    if(block&&ctx.state.doc.lineAt(ctx.pos).from===block.from)return {from:block.from,options:(options.blocks?options.blocks():options.files()).map(f=>({label:'/'+f.path,detail:f.kind||'',type:'text',apply:'/'+f.path})),validFor:/^\/[^\s]*$/};
    const tag=ctx.matchBefore(/(?:^|[\s(])#[\p{L}\p{N}_]*$/u);
    if(tag&&options.tags){const at=tag.text.indexOf('#');if(tag.text.length-at<2&&!ctx.explicit)return null;return {from:tag.from+at+1,options:options.tags().map(t=>({label:t.tag,detail:String(t.count),type:'keyword'})),validFor:/^[\p{L}\p{N}_]*$/u}}
    return null;
   }]}),
   EditorView.updateListener.of(update=>{if(update.docChanged)options.change(view.state.doc.toString());if(update.selectionSet)options.selection(view.state.selection.main)}),
   EditorView.domEventHandlers({
    paste:(event,view)=>{const text=event.clipboardData?.getData('text/plain');const author=text&&options.pasteAuthor?.(text);if(!author||view.state.readOnly)return false;event.preventDefault();view.dispatch(view.state.replaceSelection(text),{userEvent:'input.paste',annotations:pasteAuthor.of(author),scrollIntoView:true});return true},
    click:(event)=>{const thread=event.target.closest('[data-thread]');if(thread){options.openComment?.(thread.dataset.thread)}const target=event.target.closest('[data-target]');if(target&&(event.metaKey||event.ctrlKey)){options.openLink?.(target.dataset.target);return true}return false},
    mouseup:()=>{options.selection(view.state.selection.main);return false},keyup:()=>{options.selection(view.state.selection.main);return false},
    keydown:(event)=>{options.typing?.(event);return false}
   })
  ]})});
  const quote=(r,text)=>({...r,quote:text.slice(r.from,r.to),prefix:text.slice(Math.max(0,r.from-40),r.from),suffix:text.slice(r.to,r.to+40)});
  return {view,element:view.dom,text:()=>view.state.doc.toString(),selection:()=>view.state.selection.main,
   focus:()=>view.focus(),destroy:()=>view.destroy(),
   find:()=>openSearchPanel(view),
   undo:()=>undo(view),redo:()=>redo(view),complete:()=>startCompletion(view),
   restore:(anchor,head)=>view.dispatch({selection:{anchor:Math.min(anchor,view.state.doc.length),head:Math.min(head,view.state.doc.length)}}),
   setText:text=>view.dispatch({changes:{from:0,to:view.state.doc.length,insert:text}}),
   // A remote update maps the existing caret, selection, anchors and undo stack
   // through only the changed span. It is never an undoable local edit.
   syncText:text=>{const before=view.state.doc.toString();if(before===text)return;let from=0,end=before.length,nextEnd=text.length;while(from<end&&from<nextEnd&&before[from]===text[from])from++;while(end>from&&nextEnd>from&&before[end-1]===text[nextEnd-1]){end--;nextEnd--}view.dispatch({changes:{from,to:end,insert:text.slice(from,nextEnd)},annotations:Transaction.addToHistory.of(false)})},
   setReadOnly:value=>view.dispatch({effects:readOnly.reconfigure(EditorState.readOnly.of(value))}),
   // focus: 'off' | 'sentence' | 'paragraph'; typewriter keeps the caret centred
   setFocus:(scope,centred)=>{view.dispatch({effects:[focus.reconfigure(focusFields[scope]||[]),writer.reconfigure(centred?typewriter:[])]});if(centred)view.dispatch({effects:EditorView.scrollIntoView(view.state.selection.main.head,{y:'center'})})},
   setLayer:(name,ranges)=>view.dispatch({effects:setLayer.of({name,ranges})}),
   showAuthors:on=>view.dispatch({effects:authors.reconfigure(on?authorView:[])}),
   authors:()=>{const text=view.state.doc.toString();return view.state.field(authorship).map(r=>quote(r,text))},
   setAuthors:ranges=>view.dispatch({effects:setAuthors.of(ranges)}),
   markAuthor:(from,to,author)=>view.dispatch({effects:markAuthor.of({from,to,author})}),
   mark:items=>view.dispatch({effects:setAnchors.of(items)}),
   locate:(from,to)=>{view.dispatch({selection:{anchor:from,head:to},effects:EditorView.scrollIntoView(from,{y:'center'})});view.focus()},
   replace:(from,to,text)=>view.dispatch({changes:{from,to,insert:text},selection:{anchor:from+text.length}}),
   // the first line in view, for the preview to follow; and the reverse
   topLine:()=>view.state.doc.lineAt(view.lineBlockAtHeight(view.scrollDOM.scrollTop-view.documentTop+view.scrollDOM.getBoundingClientRect().top).from).number,
   scrollToLine:n=>{const line=view.state.doc.line(Math.max(1,Math.min(n,view.state.doc.lines)));view.dispatch({effects:EditorView.scrollIntoView(line.from,{y:'start'})})}
  };
 }
};
