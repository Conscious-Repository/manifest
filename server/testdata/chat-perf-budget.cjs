// chat-perf-budget.cjs — the chat surface's performance budgets, measured by
// tools/perf/chat-perf.cjs (real front end, the app's own cache headers,
// fixture data). Budgets sit well above the measured values so a real
// regression turns the suite red without making it flaky: counts are near
// deterministic and pinned tight; times are pinned loose (the suite runs
// packages in parallel on a busy machine).
//
// Measured 2026-09-27 on metis — see the plan's perf table for the history.
const assert=require('node:assert/strict'),path=require('node:path');
const {measure,report}=require(path.join(__dirname,'../../tools/perf/chat-perf.cjs'));
(async()=>{
 const out=await measure({idle:15});
 report(out);
 const within=(k,max)=>assert.ok(out[k]<=max,k+' = '+out[k]+', budget '+max);
 within('coldOpenMs',2000);
 within('warmOpenMs',2000);
 within('switchMs',750);
 within('coldOpenRequests',140);
 within('warmOpenRequests',140);
 // a second tab takes its scripts and styles from the HTTP cache: 21 reach
 // the server (the shell and the API), where every asset used to revalidate (109)
 within('warmOpenNetwork',35);
 within('idleRequestsPerMin',60);
 within('heapMB',60);
 within('nodes',30000);
 within('tilesIdleRequestsPerMin',300);
 console.log('PASS: chat perf within budget.');
})().catch(e=>{console.error(e);process.exitCode=1;});
