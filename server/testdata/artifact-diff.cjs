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

vm.runInContext(src.slice(src.indexOf('function artifactWorkingDiffFiles(')),ctx);
const snapshot='Working folder: /fixture\n\ndiff --git a/one.txt b/one.txt\n--- a/one.txt\n+++ b/one.txt\n@@ -1,2 +1,2 @@ first\n unchanged\n-old\n+new\n@@ -20,0 +21,2 @@ next\n+extra\n+line\n\\ No newline at end of file\ndiff --git a/two.txt b/two.txt\n--- a/two.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-last\n';
const parsed=ctx.artifactWorkingDiffFiles(snapshot),lines=snapshot.split('\n');
assert.equal(parsed.files.length,2);assert.equal(parsed.files[0].hunks.length,2);
for(const file of parsed.files)for(const hunk of file.hunks){assert.equal(hunk.valid,true);assert.equal(lines.slice(hunk.start-1,hunk.end).join('\n'),[hunk.header,...hunk.lines].join('\n'));}
assert.equal(parsed.files[1].path,'two.txt');assert.equal(parsed.files[1].hunks[0].after,0);
const incomplete=ctx.artifactWorkingDiffFiles('diff --git a/x b/x\n@@ -1,2 +1,2 @@\n-one\n+two\n');assert.equal(incomplete.files[0].hunks[0].valid,false);
const blank=ctx.artifactWorkingDiffFiles('diff --git a/x b/x\n@@ -1 +1 @@\n \n');assert.equal(blank.files[0].hunks[0].valid,true);assert.equal(blank.files[0].hunks[0].end,3,'a blank context line remains in the anchor');
const quoted=ctx.artifactWorkingDiffFiles('diff --git "a/a b.txt" "b/a b.txt"\n--- "a/a b.txt"\n+++ "b/a b.txt"\n@@ -1 +1 @@\n-x\n+y\n');assert.equal(quoted.files[0].path,'a b.txt');
const combined=ctx.artifactWorkingDiffFiles('diff --cc file.txt\n@@@ -1 -1 +1 @@@\n++merge\n');assert.equal(combined.files[0].combined,true);assert.equal(combined.files[0].hunks.length,0);assert.ok(combined.files[0].headers.includes('++merge'));
console.log('Hunk ranges select exact snapshot bytes; deletions, zero counts, blank lines, quoted paths and unsupported merge diffs passed');
