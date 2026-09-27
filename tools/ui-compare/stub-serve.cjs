#!/usr/bin/env node
// stub-serve.cjs — the real front end over the chat stub
// (server/testdata/chat-stub-api.cjs: two Alfred threads, the coding agents'
// model catalog) on a fixed port, for ui-compare shots of chat screens
// without touching the owner's live conversations.
//
//   node tools/ui-compare/stub-serve.cjs [port]      (default 7790)
const path=require('node:path');
const {makeStub}=require(path.join(__dirname,'../../server/testdata/chat-stub-api.cjs'));
const port=Number(process.argv[2]||7790);
makeStub().server.listen(port,'127.0.0.1',()=>console.log('stub front end http://127.0.0.1:'+port));
