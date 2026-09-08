const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const root = require('node:path').resolve(__dirname, '../../server/web');
function context(fetch) {
  return vm.createContext({window: {}, fetch, AbortController, setTimeout, clearTimeout});
}
for (const portal of ['portal', 'ooda']) {
  function api(fetch) {
    const c = context(fetch);
    if (portal === 'portal') {
      vm.runInContext(fs.readFileSync(`${root}/portal/src/team-api.js`, 'utf8'), c);
      return body => c.window.TEAM_API.post('/test', body);
    }
    vm.runInContext(fs.readFileSync(`${root}/ooda/src/data-load.js`, 'utf8'), c);
    vm.runInContext(fs.readFileSync(`${root}/ooda/src/team-api.js`, 'utf8'), c);
    return body => vm.runInContext('postJSON("/test", {})', c).then(value => ({ok:true,value}), error => ({ok:false,error:error.message}));
  }
  test(`${portal}: redirected login never counts as a successful send`, async () => {
    const send = api(async () => ({ok:true, redirected:true, status:200, json:async () => {throw Error('HTML');}}));
    const r = await send({text:'draft'});
    assert.equal(r.ok, false); assert.match(r.error, /session expired/);
  });
  test(`${portal}: network failure resolves to a failed send without retrying`, async () => {
    let calls=0;
    const send=api(async () => {calls++; throw new TypeError('Failed to fetch');});
    assert.equal((await send({})).ok, false); assert.equal(calls,1);
  });
  test(`${portal}: successful write returns server result`, async () => {
    const send=api(async () => ({ok:true,status:200,json:async()=>({id:'accepted'})}));
    assert.equal((await send({})).value.id, 'accepted');
  });
}
test('task routes accept encoded slashes and either hash convention', () => {
  const c=context(()=>{}); c.location={hash:''}; c.window.location=c.location;
  const a=fs.readFileSync(`${root}/portal/src/app.jsx`,'utf8');
  vm.runInContext(a.slice(a.indexOf('function decodeHashID'),a.indexOf('function PortalApp')),c);
  const o=fs.readFileSync(`${root}/ooda/src/app.jsx`,'utf8');
  vm.runInContext(o.slice(o.indexOf('const OODA_VIEWS'),o.indexOf('function App()')),c);
  for (const hash of ['#item/aion-bl%2Ftask', '#/item/aion-bl%2Ftask', '#/item/aion-bl/task']) {
    c.location.hash=hash;
    assert.equal(vm.runInContext('parseHash().itemId',c),'aion-bl/task');
    assert.equal(vm.runInContext('parseOodaHash().item',c),'aion-bl/task');
  }
});
