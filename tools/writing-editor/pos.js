// Parts of speech for Writing's syntax highlight (iA Writer's): loaded only
// when the owner turns it on. compromise (MIT) tags English on the device.
import nlp from 'compromise';
const order=[['Conjunction','conjunction'],['Adverb','adverb'],['Adjective','adjective'],['Verb','verb'],['Noun','noun']];
window.ManifestPOS={
 // [{from,to,pos}] in the text's own character offsets
 tag(text){
  const out=[];
  for(const s of nlp(text).json({offset:true,terms:{offset:true}}))for(const t of s.terms){
   const tags=new Set(t.tags||[]);if(tags.has('Pronoun')||tags.has('Determiner')||tags.has('Preposition'))continue;
   const hit=order.find(([tag])=>tags.has(tag));if(!hit||!t.offset)continue;
   const from=t.offset.start,len=t.offset.length;if(len>0)out.push({from,to:from+len,pos:hit[1]});
  }
  return out;
 }
};
