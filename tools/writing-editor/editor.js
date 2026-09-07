import {EditorState, StateEffect, StateField, Compartment} from '@codemirror/state';
import {EditorView, Decoration, keymap, WidgetType} from '@codemirror/view';
import {defaultKeymap, history, historyKeymap, indentWithTab} from '@codemirror/commands';
import {markdown, markdownKeymap} from '@codemirror/lang-markdown';
import {syntaxHighlighting, HighlightStyle, syntaxTree, bracketMatching} from '@codemirror/language';
import {tags} from '@lezer/highlight';
import {autocompletion, completionKeymap} from '@codemirror/autocomplete';
import {searchKeymap, highlightSelectionMatches, openSearchPanel} from '@codemirror/search';

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
// Hide presentation punctuation only outside the active lines. The document
// remains Markdown; cursor entry reveals its syntax without serialization.
function metadataEnd(state) {
 const first=state.doc.line(1);if(first.text!=='---')return 0;
 for(let i=2;i<=state.doc.lines;i++){const line=state.doc.line(i);if(line.text==='---'||line.text==='...')return Math.min(line.to+1,state.doc.length)}return 0;
}
class PropertiesWidget extends WidgetType {
 toDOM(){const node=document.createElement('div');node.className='write-properties';node.textContent='properties · edit in source mode';return node}
}
function presentation(state){
 const ranges=[], selection=state.selection.main, fmEnd=metadataEnd(state);
 const first=state.doc.lineAt(selection.from).number,last=state.doc.lineAt(selection.to).number;
 if(fmEnd>0)ranges.push(Decoration.replace({widget:new PropertiesWidget(),block:true}).range(0,fmEnd));
 syntaxTree(state).iterate({enter:n=>{
  if(n.from<fmEnd||!['HeaderMark','EmphasisMark','StrikethroughMark'].includes(n.name))return;
  const line=state.doc.lineAt(n.from);if(line.number>=first&&line.number<=last)return;
  let end=n.to;if(n.name==='HeaderMark'&&state.doc.sliceString(end,end+1)===' ')end++;
  ranges.push(Decoration.replace({}).range(n.from,end));
 }});
 const re=/\[\[([^\]\n]+)\]\]/g;
 for(let i=1;i<=state.doc.lines;i++){const line=state.doc.line(i);if(line.from<fmEnd)continue;let match;
  while((match=re.exec(line.text))){const from=line.from+match.index,to=from+match[0].length;
   ranges.push(Decoration.mark({class:'write-wikilink',attributes:{'data-target':match[1].split('|')[0]}}).range(from,to));
  }
 }
 return Decoration.set(ranges,true);
}
const livePreview=StateField.define({create:presentation,update:(value,tr)=>(tr.docChanged||tr.selection)?presentation(tr.state):value,provide:field=>[EditorView.decorations.from(field),EditorView.atomicRanges.of(view=>view.state.field(field))]});

window.ManifestEditor = {
 create(parent,options){
  const mode=new Compartment(), readOnly=new Compartment();
  const view=new EditorView({parent,state:EditorState.create({doc:options.text,selection:{anchor:(options.text.match(/^---\n[\s\S]*?\n(?:---|\.\.\.)\n/)||[''])[0].length},extensions:[
   history(), markdown(), bracketMatching(), syntaxHighlighting(style), EditorView.lineWrapping,
   anchors, highlightSelectionMatches(), mode.of(livePreview),
   readOnly.of(EditorState.readOnly.of(!!options.readOnly)),
   EditorView.contentAttributes.of({'aria-label':'Document editor',spellcheck:'true'}),
   keymap.of([{key:'Mod-s',run:()=>{options.save();return true}},{key:'Mod-Alt-m',run:()=>{options.comment();return true}},...defaultKeymap,...historyKeymap,...markdownKeymap,...completionKeymap,...searchKeymap,indentWithTab]),
   autocompletion({override:[ctx=>{
    const word=ctx.matchBefore(/\[\[[^\]\n]*/);if(!word)return null;
    return {from:word.from+2,options:options.files().map(f=>({label:f.name,detail:f.path,type:'text',apply:f.path.replace(/\.md$/i,'')+']]'})),validFor:/[^\]\n]*/};
   }]}),
   EditorView.updateListener.of(update=>{if(update.docChanged)options.change(view.state.doc.toString());if(update.selectionSet)options.selection(view.state.selection.main)}),
   EditorView.domEventHandlers({click:(event)=>{const thread=event.target.closest('[data-thread]');if(thread){options.openComment?.(thread.dataset.thread)}const target=event.target.closest('[data-target]');if(target&&(event.metaKey||event.ctrlKey)){options.openLink?.(target.dataset.target);return true}return false},mouseup:()=>{options.selection(view.state.selection.main);return false},keyup:()=>{options.selection(view.state.selection.main);return false}})
  ]})});
  return {view,element:view.dom,text:()=>view.state.doc.toString(),selection:()=>view.state.selection.main,
   focus:()=>view.focus(),destroy:()=>view.destroy(),
   find:()=>openSearchPanel(view),
   restore:(anchor,head)=>view.dispatch({selection:{anchor:Math.min(anchor,view.state.doc.length),head:Math.min(head,view.state.doc.length)}}),
   setText:text=>view.dispatch({changes:{from:0,to:view.state.doc.length,insert:text}}),
   setSource:source=>view.dispatch({effects:mode.reconfigure(source?[]:livePreview)}),
   setReadOnly:value=>view.dispatch({effects:readOnly.reconfigure(EditorState.readOnly.of(value))}),
   mark:items=>view.dispatch({effects:setAnchors.of(items)}),
   locate:(from,to)=>{view.dispatch({selection:{anchor:from,head:to},effects:EditorView.scrollIntoView(from,{y:'center'})});view.focus()},
   replace:(from,to,text)=>view.dispatch({changes:{from,to,insert:text},selection:{anchor:from+text.length}})
  };
 }
};
