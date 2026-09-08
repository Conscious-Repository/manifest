// The same renderer and financial calculations as Manifest, fed by a
// signed-in, deal-scoped endpoint. Unmounting cancels refresh work.
function ViewUnderwriting({slug}) {
  const host=React.useRef(null);
  React.useEffect(()=>renderDealDiligence(host.current,slug,{
    endpoint:'/api/ooda/deal/'+encodeURIComponent(slug)+'/underwriting',
    onBack:()=>{window.location.hash='#/dashboard';}
  }),[slug]);
  return <div ref={host} className="ooda-underwriting" />;
}
