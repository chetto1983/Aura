const CACHE_NAME="aura-precache-1a96c216afabe3d9";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"5508e1ffcdd6c2eb"},{"url":"/assets/AppShell-CW-xxPE7.js","revision":"ac3081419308f947"},{"url":"/assets/assistant-ui-D7JgFyK7.js","revision":"a123bba29a8c38f8"},{"url":"/assets/authConfig-DbZ1HiAW.js","revision":"6971db49d5c35988"},{"url":"/assets/ExternalStoreChat-sNpwAOTK.js","revision":"0c1206c79d1ce465"},{"url":"/assets/focusTrap-CT7efBc1.js","revision":"5a63c28b9262d5da"},{"url":"/assets/governanceApi-B9C26qFY.js","revision":"9a8168b9e1a501d7"},{"url":"/assets/GovernanceWorkspace-BZuA4QUD.js","revision":"43e83179770b3c45"},{"url":"/assets/GraphExplorer-D8MLnNEU.js","revision":"dcc8f67a0dbcfe87"},{"url":"/assets/index-aeYJ-P8j.css","revision":"98387d03fafd25b9"},{"url":"/assets/index-D64nRQ4N.js","revision":"05181fdb9cdecff6"},{"url":"/assets/json-7kOhAfOp.js","revision":"6363726edc6a73e0"},{"url":"/assets/LoginPage-OmzW1tWH.js","revision":"58e029bb5a49745d"},{"url":"/assets/McpBoard-BKIPz5tF.js","revision":"2bed69597a295cc5"},{"url":"/assets/NotFoundView-XVnb4DkN.js","revision":"20c97970c3cc2ae2"},{"url":"/assets/OnboardingWizard-C8Qcj3YM.js","revision":"6f1178769565d5b5"},{"url":"/assets/rolldown-runtime-BUR9erT_.js","revision":"66bc2dc82ec39095"},{"url":"/assets/SchedulerBoard-B-Szk-9k.js","revision":"545770af4e9eae2b"},{"url":"/assets/shiki-core-BZGQo0dP.js","revision":"855d00a071c225de"},{"url":"/assets/shiki-lang-js-KbSxL_Iu.js","revision":"e81d53e8da7ecf78"},{"url":"/assets/shiki-lang-tools-BNhm00f6.js","revision":"ba6200b72fd342ee"},{"url":"/assets/shiki-lang-web-CTkMxnFF.js","revision":"68fbe9995ce02ec1"},{"url":"/assets/shiki-themes-_UjgcaiI.js","revision":"6fbd6a859ed63c33"},{"url":"/assets/SigmaCanvas-D72VHEEe.js","revision":"a3dc12fad0ea50dc"},{"url":"/assets/SigmaCanvas-gwv2EvJr.css","revision":"20e692f226ef2093"},{"url":"/assets/SkillsBoard-aWh9V0nq.js","revision":"4f06d9c7611a88c5"},{"url":"/assets/sourceExplorerControls-BFvIgvZN.js","revision":"1e0df4a5b3d8630c"},{"url":"/assets/Spinner-DsQPR6gX.js","revision":"b66515dfda37df10"},{"url":"/assets/useMutation-Cq00q8Gg.js","revision":"97cf07a222bb493a"},{"url":"/assets/useQuery-CZhxOqen.js","revision":"67ee2e876701254c"},{"url":"/assets/useTranslation-CK6doXbe.js","revision":"0967c389cc9882da"},{"url":"/favicon.svg","revision":"7b17af2ead77936a"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"3a814571ad551c73"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"c32d08b35f2cc4e2"},{"url":"/pwa-512.png","revision":"07720c285ef365f3"},{"url":"/pwa-maskable-512.png","revision":"33eae128615a06b6"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
self.addEventListener('install',(event)=>{
  event.waitUntil(caches.open(CACHE_NAME).then((cache)=>Promise.all(PRECACHE.map((entry)=>cache.add(entry.url).catch(()=>undefined)))).then(()=>self.skipWaiting()));
});
self.addEventListener('activate',(event)=>{
  event.waitUntil(caches.keys().then((keys)=>Promise.all(keys.filter((key)=>key.startsWith('aura-precache-')&&key!==CACHE_NAME).map((key)=>caches.delete(key)))).then(()=>self.clients.claim()));
});
self.addEventListener('fetch',(event)=>{
  const request=event.request;
  if(request.method!=='GET') return;
  const url=new URL(request.url);
  if(url.origin!==self.location.origin) return;
  if(request.mode==='navigate'){
    event.respondWith(fetch(request).catch(()=>caches.match('/index.html')));
    return;
  }
  event.respondWith(caches.match(request).then((cached)=>cached||fetch(request)));
});
