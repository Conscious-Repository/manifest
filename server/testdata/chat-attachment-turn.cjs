// chat-attachment-turn.cjs — a user turn that carried attachments (2026-09-12):
// the model's context (the [context-file::] token, the server's attachment
// block, the CLI's "[Image #N]" paste marker) never reaches the reader; the
// turn shows the message and one preview card per file, in both the native
// (terminal) prompt line and the agent-chat turn; metadata is fetched once
// per file however often the turn repaints. Run: node server/testdata/chat-attachment-turn.cjs
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const src=fs.readFileSync(path.join(__dirname,'../web/js/48-chat.js'),'utf8');
const slice=(start,end)=>{const s=src.indexOf(start);assert.ok(s>=0,'missing '+start);const e=src.indexOf(end,s+1);assert.ok(e>s,'missing '+end);return src.slice(s,e);};
// a small DOM: enough for el(), append, replaceWith, textContent, classList
const node=(tag)=>({tag,className:'',children:[],attrs:{},dataset:{},_text:'',isConnected:true,
 append(...items){items.forEach(i=>{if(typeof i==='string')this._text+=i;else{i.parent=this;this.children.push(i);}});},
 replaceWith(other){const i=this.parent.children.indexOf(this);this.parent.children[i]=other;other.parent=this.parent;this.isConnected=false;},
 setAttribute(k,v){this.attrs[k]=v;},get textContent(){return this._text+this.children.map(c=>c.textContent).join('');},set textContent(v){this._text=v;this.children=[];},
 classList:{add(){},toggle(){}},find(cls){const out=[];const walk=n=>{n.children.forEach(c=>{if(c.className.split(' ').includes(cls))out.push(c);walk(c);});};walk(this);return out;}});
let fetches=[];
const ctx=vm.createContext({
 el:(tag,cls,text)=>{const e=node(tag);e.className=cls||'';if(text!==undefined)e._text=text;return e;},
 document:{createElement:tag=>node(tag)},chatFileHref:h=>'/attach/'+h,chatOpenAttachment(){},chatQuestionReplyDisplay:t=>t,
 chatTermPromptGlyph:'❯',fmtWhen:()=>'12:30 PM',
 fetch:async url=>{fetches.push(url);return {ok:true,status:200,json:async()=>({id:'e81896adb9df2b7d33f54b26d2364b33',name:'IMG_4505.png',size:239705,type:'image/png'})};},
});
vm.runInContext(slice('const chatFileTokenRe','\n// chatHead — the thread head'),ctx);
vm.runInContext(slice('function chatTermCmdLine(t)','\n}\n')+'\n}',ctx);
const sent='[Image #5]Small bug here where a message looks double sent\n[context-file:: e81896adb9df2b7d33f54b26d2364b33]\n<!-- manifest-chat-attachment-context -->\nAttached reference files: inspect these files.\n- IMG_4504.png (206895 bytes): /home/x/content.png\n<!-- /manifest-chat-attachment-context -->\n';
(async()=>{
 // the split: reader text vs. files
 const split=ctx.chatSplitUserMessage(sent);
 assert.equal(split.text,'Small bug here where a message looks double sent');
 assert.equal(JSON.stringify(split.files.map(f=>f.id)),'["e81896adb9df2b7d33f54b26d2364b33"]');
 assert.equal(ctx.chatSplitUserMessage('[Image #1][Image #2] two pictures').text,'two pictures');
 // Claude Code re-wraps the block: no path after the file line, the closing marker split over two lines
 const claude='tweak the tab titles\n<!-- manifest-chat-attachment-context -->\nAttached reference files: inspect these files.\n- Screenshot 2026-09-12 at 12.49.39\u202fPM.png (35365 bytes):\n<!--\n/manifest-chat-attachment-context -->';
 assert.equal(ctx.chatSplitUserMessage(claude).text,'tweak the tab titles','the re-wrapped block is stripped too');
 assert.equal(ctx.chatSplitUserMessage('see this\n<!-- manifest-chat-attachment-context -->\nAttached reference files\n- a.png (1 bytes):').text,'see this','a block without its closing marker drops to the end');
 assert.equal(ctx.chatSplitUserMessage('plain text, no files').text,'plain text, no files');
 assert.equal(JSON.stringify(ctx.chatSplitUserMessage('see\n[file:: '+'a'.repeat(64)+' notes.pdf]').files),JSON.stringify([{hash:'a'.repeat(64),name:'notes.pdf'}]));
 // the native prompt line: previews lead, the text is clean, the time stays
 const line=ctx.chatTermCmdLine({text:sent,ts:'2026-09-12T12:30:00Z'});
 assert.equal(line.find('chat-term-cmd-text')[0].textContent,'Small bug here where a message looks double sent');
 assert.equal(line.textContent.includes('manifest-chat-attachment-context'),false,'the context block never reaches the reader');
 assert.equal(line.textContent.includes('[Image #5]'),false);
 assert.equal(line.children[1].className,'chat-attach-chips','previews sit ahead of the text');
 await new Promise(r=>setTimeout(r,0));
 const card=line.find('chat-attachment-card')[0];
 assert.equal(card.find('chat-attachment-name')[0].textContent,'IMG_4505.png','the card takes the file name from metadata');
 assert.equal(card.children[0].children[0].tag,'img','an image file shows a thumbnail');
 assert.equal(card.children[0].children[0].loading,'lazy');assert.equal(card.children[0].children[0].decoding,'async');
 assert.equal(card.find('chat-attachment-size')[0].textContent,'235 KB');
 // the agent-chat turn does the same
 const turn=ctx.chatUserTurn(sent);await new Promise(r=>setTimeout(r,0));
 assert.equal(turn._text,'Small bug here where a message looks double sent');
 assert.equal(turn.find('chat-attachment-card').length,1);
 // metadata: one fetch per file across every repaint
 assert.equal(fetches.length,1,'one metadata fetch per file');
 for(let n=0;n<5;n++){ctx.chatTermCmdLine({text:sent});ctx.chatUserTurn(sent);}
 await new Promise(r=>setTimeout(r,0));
 assert.equal(fetches.length,1,'repaints reuse the cached metadata');
 // a removed file: the card says so and stays inert
 ctx.fetch=async url=>{fetches.push(url);return {ok:false,status:404};};
 const gone=ctx.chatUserTurn('bye\n[context-file:: '+'f'.repeat(32)+']');await new Promise(r=>setTimeout(r,0));
 assert.equal(gone.find('chat-attachment-size')[0].textContent,'File removed');assert.equal(gone.find('chat-attachment-open')[0].disabled,true);
 console.log('PASS: attachment turns show the message and preview cards, hide the model context, and fetch metadata once.');
})().catch(e=>{console.error(e);process.exit(1);});
