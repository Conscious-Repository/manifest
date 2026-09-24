const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const code=fs.readFileSync('web/js/70-note.js','utf8');
const requested=[];
const ctx=vm.createContext({els:{artifactView:{},artifactRendered:{appendChild(){}},artifactSource:{},artifactTitle:{}},renderMarkdown:text=>text,fetch:async url=>{requested.push(url);return {ok:true,json:async()=>({summary:{spirit:'research',ritual:'brief'},body:'exact report'})};}});
vm.runInContext(code.slice(code.indexOf('let _artifactRaw'),code.indexOf('// artifactTitleFromRef')),ctx);
(async()=>{
 await ctx.showArtifact('run-in/team%20tree/same-run');
 assert.equal(requested.pop(),'/api/spirits/runs/same-run?harness=team%20tree');
 await ctx.showArtifact('run/old-run');
 assert.equal(requested.pop(),'/api/spirits/runs/old-run');
 await ctx.showArtifact('run-in//same-run');
 assert.equal(requested.length,0,'missing harness cannot fall back to an unrelated source');
 assert.match(ctx.els.artifactRendered.textContent,/harness is required/);
 console.log('Exact harness run routes and legacy run routes passed');
})().catch(e=>{console.error(e);process.exitCode=1});
