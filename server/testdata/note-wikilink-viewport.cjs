// note-wikilink-viewport.cjs — the [[ typeahead popup stays inside what the
// reader can see (2026-09-13): below the caret line when that fits in the
// visible band, else above it; capped to the room it has; the band follows
// the visual viewport when a phone keyboard shortens and pans it; and a fixed
// popup that lands off its target (panned viewport) is corrected once.
// Run: node server/testdata/note-wikilink-viewport.cjs
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const src=fs.readFileSync(path.join(__dirname,'../web/js/70-note.js'),'utf8');
const slice=(start,end)=>{const s=src.indexOf(start);assert.ok(s>=0,'missing '+start);const e=src.indexOf(end,s+1);assert.ok(e>s,'missing '+end);return src.slice(s,e);};
let rect={top:300,bottom:340,left:40},natural=200,landedShift=0;
const popup={style:{},offsetWidth:260,get offsetHeight(){return natural;},getBoundingClientRect(){return {top:parseFloat(popup.style.top)+landedShift};}};
const ctx=vm.createContext({window:{innerWidth:390,innerHeight:844,visualViewport:null},
 caretCoords:(ta)=>({left:rect.left,top:rect.bottom+2,lineTop:rect.top}),_wlPopup:popup});
vm.runInContext(slice('function wlVisibleRange(anchor)','\n// caretCoords returns'),ctx);
const ta={getBoundingClientRect:()=>rect};
const place=()=>{popup.style={};ctx.wlPosition(ta);return {top:parseFloat(popup.style.top),left:parseFloat(popup.style.left),max:popup.style.maxHeight||''};};
// 1. desktop, no keyboard: below the field, uncapped
let p=place();assert.equal(p.top,342);assert.equal(p.max,'');assert.equal(p.left,40);
// 2. phone keyboard, client rects counted from the layout viewport: the field sits low in the panned band, the list flips above it
ctx.window.visualViewport={offsetTop:300,height:544,width:390};rect={top:700,bottom:740,left:40};natural=360;
p=place();assert.equal(p.top,700-2-360,'flips above the field');assert.equal(p.max,'','room above holds the whole list');
// 3. near the top of the band: stays below
rect={top:340,bottom:380,left:40};p=place();assert.equal(p.top,382);
// 4. little room either way: the larger side wins and the list is capped to it
rect={top:520,bottom:560,left:40};natural=360;
p=place();assert.equal(p.max,'274px','the larger side (below: 844-8-562) wins and caps the list');assert.equal(p.top,562);
rect={top:640,bottom:680,left:40};p=place();assert.equal(p.max,'330px','above when it is the larger side (640-2-308), capped');assert.equal(p.top,640-2-330);
assert.ok(p.top>=308,'never above the visible band');
// 5. the popup lands lower than asked (fixed vs. panned viewport): corrected once
landedShift=50;rect={top:700,bottom:740,left:40};p=place();assert.equal(p.top,700-2-360-50,'corrected by the measured offset');landedShift=0;
// 6. client rects counted from the visual viewport: the band that holds the field is chosen
rect={top:200,bottom:240,left:40};natural=100;p=place();assert.equal(p.top,242,'below, inside the 0…544 band');
const vis=ctx.wlVisibleRange({top:200,bottom:240});assert.equal(vis.top,0);assert.equal(vis.bottom,544);
// 7. the popup keeps 12px from the right edge of the visual viewport
rect={top:200,bottom:240,left:300};p=place();assert.equal(p.left,390-260-12);
console.log('PASS: the [[ popup flips, caps and corrects itself to stay inside the visible band.');
