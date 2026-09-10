const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const src=fs.readFileSync(require('node:path').join(__dirname,'../web/js/05-components.js'),'utf8');
const ctx=vm.createContext({});
vm.runInContext(src.slice(src.indexOf('function artifactLineChanges('),src.indexOf('// A shared, version-aware workspace.')),ctx);
const compare=(a,b)=>ctx.artifactLineChanges(a,b);
for(const before of ['', 'one', 'one\n', 'one\ntwo', '<script>alert(1)</script>', 'same\nsame\nend']){
 for(const after of ['', 'one', 'one\n', 'one\nthree\ntwo', 'same\nend', '  indented\n']){
  const changes=compare(before,after);
  assert.equal(changes.filter(l=>l.kind!=='added').map(l=>l.text).join('\n'),before);
  assert.equal(changes.filter(l=>l.kind!=='removed').map(l=>l.text).join('\n'),after);
 }
}
const changed=compare('start\nold\nend','start\nnew\nend');
assert.deepEqual(Array.from(changed,l=>l.kind),['same','removed','added','same']);
const before='prefix\n'+Array.from({length:700},(_,i)=>'old '+i).join('\n')+'\nsuffix';
const after='prefix\n'+Array.from({length:700},(_,i)=>'new '+i).join('\n')+'\nsuffix';
const large=compare(before,after);
assert.equal(large.filter(l=>l.kind!=='added').map(l=>l.text).join('\n'),before);
assert.equal(large.filter(l=>l.kind!=='removed').map(l=>l.text).join('\n'),after);
console.log('Exact revision reconstruction, empty/trailing lines, and bounded large replacements passed');
