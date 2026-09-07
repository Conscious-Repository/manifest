const fs=require('node:fs'),path=require('node:path');
const root=path.join(__dirname,'node_modules');let entries=[];
for(const name of fs.readdirSync(root).sort()){
 if(name.startsWith('.'))continue;
 if(name.startsWith('@'))for(const child of fs.readdirSync(path.join(root,name)).sort())entries.push(path.join(root,name,child));
 else entries.push(path.join(root,name));
}
const texts=[];
for(const dir of entries){const p=path.join(dir,'package.json');if(!fs.existsSync(p))continue;const d=JSON.parse(fs.readFileSync(p,'utf8'));for(const name of ['LICENSE','LICENSE.md','LICENSE.txt']){const file=path.join(dir,name);if(fs.existsSync(file)){texts.push(d.name+' '+d.version+'\n'+fs.readFileSync(file,'utf8'));break}}}
fs.writeFileSync(path.join(__dirname,'../../server/web/vendor/writing-editor.LICENSE.txt'),texts.join('\n\n'));
