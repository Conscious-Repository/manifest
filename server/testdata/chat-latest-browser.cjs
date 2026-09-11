const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
(async()=>{const browser=await chromium.launch({headless:true,channel:'chrome'});try{
 const page=await browser.newPage({viewport:{width:1000,height:800}}),root=path.join(__dirname,'../web');
 await page.setContent('<main class="chat-main" style="height:700px"><div id="chatTranscript" style="overflow:auto;flex:1"><div style="height:2000px">Long conversation</div></div><div id="chatComposer"><textarea aria-label="Message"></textarea></div></main>');
 for(const file of ['00-core','05-primitives','48-chat'])await page.addStyleTag({content:fs.readFileSync(path.join(root,'css',file+'.css'),'utf8')});
 await page.evaluate(()=>Object.assign(window,{chatScrollBound:false,chatReadingGestureUntil:0,chatLastY:0,chatStick:true,chatSaveReadingPosition:()=>{},el:(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls;e.textContent=text;return e;}}));
 const source=fs.readFileSync(path.join(root,'js/48-chat.js'),'utf8'),workspace=fs.readFileSync(path.join(root,'js/49-chat-workspace.js'),'utf8');
 await page.addScriptTag({content:source.slice(source.indexOf('function bindChatScroll()'),source.indexOf('// parseChatTurns splits'))});
 await page.addScriptTag({content:workspace.slice(workspace.indexOf('function chatUpdateJump()'))});
 await page.evaluate(()=>{bindChatScroll();chatPin();});
 const jump=page.getByRole('button',{name:'Jump to latest messages'});assert.equal(await jump.isVisible(),false);
 await page.locator('#chatTranscript').evaluate(e=>{e.scrollTop=100;e.dispatchEvent(new Event('scroll'));});await jump.waitFor();
 await jump.click();assert.equal(await jump.isVisible(),false);assert.equal(await page.getByLabel('Message',{exact:true}).evaluate(e=>e===document.activeElement),true);
 await page.setViewportSize({width:390,height:844});await page.locator('#chatTranscript').evaluate(e=>{e.scrollTop=0;e.dispatchEvent(new Event('scroll'));});await jump.waitFor();
 const b=await jump.boundingBox(),t=await page.locator('#chatTranscript').boundingBox();assert.ok(b.y>=t.y&&b.y+b.height<=t.y+t.height);
 await page.evaluate(()=>{document.querySelector('.chat-main').classList.add('landing');chatUpdateJump();});assert.equal(await jump.isVisible(),false);
 console.log('PASS: scroll-aware latest action, bottom following, composer focus, phone bounds and landing suppression.');
}finally{await browser.close()}})().catch(e=>{console.error(e);process.exit(1)});
