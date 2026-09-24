const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const code=fs.readFileSync(require('node:path').join(__dirname,'../web/js/05-components.js'),'utf8');
const ctx=vm.createContext({TextEncoder});vm.runInContext(code.slice(code.indexOf('function artifactDelimitedRows')),ctx);
const parse=(s,d=',')=>JSON.parse(JSON.stringify(ctx.artifactDelimitedRows(s,d)));
assert.deepEqual(parse('\ufeffname,value\r\n"a,b","say ""hi""\r\nagain"\r\nx,\r\n'),[
 {cells:['name','value'],start:1,end:1},{cells:['a,b','say "hi"\r\nagain'],start:2,end:3},{cells:['x',''],start:4,end:4}]);
assert.deepEqual(parse('"a\tb"\t=SUM(A1)\nshort','\t'),[{cells:['a\tb','=SUM(A1)'],start:1,end:1},{cells:['short'],start:2,end:2}]);
assert.deepEqual(parse(''),[]);assert.deepEqual(parse('\n'),[{cells:[''],start:1,end:1}]);
assert.deepEqual(parse(',""'),[{cells:['',''],start:1,end:1}]);
for(const input of ['a"b','"a"b','"unclosed','a\rb','a\0b','x,'.repeat(50),'x\n'.repeat(201),'x'.repeat(16001),'é'.repeat(524289)])assert.throws(()=>parse(input),undefined,input.slice(0,30));
assert.equal(parse('x\n'.repeat(200)).length,200);
console.log('PASS: delimited records, exact physical source lines, literal values and bounded fallback');
