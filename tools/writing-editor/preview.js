// Writing's preview and export renderer (iA Writer's Markdown): loaded only
// when a preview or an export is asked for. markdown-it (MIT) with footnotes
// and ==highlight==, plus Writing's own rules: task lists, [[wikilinks]],
// content blocks (a path alone on its line), {{TOC}}, +++ page breaks, and a
// source line on every top-level block so the preview can follow the editor.
import MarkdownIt from 'markdown-it';
import footnote from 'markdown-it-footnote';
import mark from 'markdown-it-mark';

function slug(text,used){let s=text.toLowerCase().replace(/[^\p{L}\p{N}]+/gu,'-').replace(/^-|-$/g,'')||'section';let n=s,i=2;while(used.has(n))n=s+'-'+i++;used.add(n);return n}
window.ManifestMarkdown={
 // opts: link(target) → {href, missing}; block(path, caption) → HTML string or null; wikilinks: 'link' | 'text'
 render(text,opts={}){
  const md=new MarkdownIt({html:false,linkify:true,typographer:true,breaks:false}).use(footnote).use(mark);
  // content blocks and page breaks are whole lines
  md.block.ruler.before('paragraph','manifest_line',(state,start,end,silent)=>{
   const line=state.src.slice(state.bMarks[start]+state.tShift[start],state.eMarks[start]);
   let m;
   if(line.trim()==='+++'){if(!silent){const t=state.push('page_break','',0);t.map=[start,start+1]}state.line=start+1;return true}
   if(line.trim()==='{{TOC}}'){if(!silent){const t=state.push('toc','',0);t.map=[start,start+1]}state.line=start+1;return true}
   if((m=/^\/(\S+?)(?:\s+(?:"([^"]*)"|\(([^)]*)\)))?\s*$/.exec(line))&&opts.block){if(!silent){const t=state.push('content_block','',0);t.map=[start,start+1];t.meta={path:m[1],caption:m[2]||m[3]||''}}state.line=start+1;return true}
   return false;
  });
  md.inline.ruler.before('link','wikilink',(state,silent)=>{
   const src=state.src.slice(state.pos);const m=/^\[\[([^\]\n|]+)(?:\|([^\]\n]+))?\]\]/.exec(src);if(!m)return false;
   if(!silent){const t=state.push('wikilink','',0);t.meta={target:m[1].trim(),label:(m[2]||m[1]).trim()}}
   state.pos+=m[0].length;return true;
  });
  const esc=md.utils.escapeHtml,used=new Set(),headings=[];
  md.core.ruler.push('manifest_after',state=>{
   const tokens=state.tokens;
   for(let i=0;i<tokens.length;i++){const t=tokens[i];
    if(t.level===0&&t.nesting>=0&&t.map&&t.type!=='inline')t.attrSet('data-line',String(t.map[0]+1));
    if(t.type==='heading_open'){const inline=tokens[i+1];const title=inline.children.filter(c=>c.type==='text'||c.type==='code_inline').map(c=>c.content).join('');const id=slug(title,used);t.attrSet('id',id);headings.push({level:+t.tag.slice(1),title,id})}
    // a list item that starts [ ] or [x] is a task
    if(t.type==='inline'&&tokens[i-2]?.type==='list_item_open'){const first=t.children[0];const m=first&&first.type==='text'&&/^\[([ xX])\]\s+/.exec(first.content);
     if(m){first.content=first.content.slice(m[0].length);const box=new state.Token('task_box','',0);box.meta={done:m[1]!==' '};t.children.unshift(box);tokens[i-2].attrJoin('class','task')}}
   }
  });
  const r=md.renderer.rules;
  r.page_break=()=> '<div class="page-break" role="separator"></div>\n';
  r.task_box=(t,i)=>'<input type="checkbox" disabled'+(t[i].meta.done?' checked':'')+'> ';
  r.wikilink=(t,i)=>{const {target,label}=t[i].meta;if(opts.wikilinks==='text')return esc(label);const l=opts.link?opts.link(target):{href:'#'};return '<a class="wikilink'+(l.missing?' missing':'')+'" href="'+esc(l.href||'#')+'">'+esc(label)+'</a>'};
  r.content_block=(t,i)=>{const {path,caption}=t[i].meta;const html=opts.block(path,caption);return html==null?'<p class="content-block missing">/'+esc(path)+'</p>\n':html};
  r.toc=()=>{let out='<nav class="toc"><ul>';for(const h of headings)out+='<li class="toc-'+h.level+'"><a href="#'+esc(h.id)+'">'+esc(h.title)+'</a></li>';return out+'</ul></nav>\n'};
  const html=md.render(text);
  return {html,headings};
 }
};
