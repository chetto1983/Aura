const CACHE_NAME="aura-precache-97c7de4106e58674";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"5508e1ffcdd6c2eb"},{"url":"/assets/AppShell-_Ekyv5UU.js","revision":"ec2224e7e12f05b0"},{"url":"/assets/assistant-ui-D7JgFyK7.js","revision":"a123bba29a8c38f8"},{"url":"/assets/authConfig-DbDnexwx.js","revision":"b9d0f3af463e706b"},{"url":"/assets/ExternalStoreChat-CekpEPei.js","revision":"420f64f4537437e8"},{"url":"/assets/focusTrap-C0mxStbF.js","revision":"1e1d5976fad39b74"},{"url":"/assets/governanceApi-BYa8Q8iT.js","revision":"77dd0d85e0ae8812"},{"url":"/assets/GovernanceWorkspace-BpNCcBlU.js","revision":"0c52b6db7d52b5b8"},{"url":"/assets/GraphExplorer-CKezuJIS.js","revision":"7361f4ac6d754a81"},{"url":"/assets/index-DMJ9gAUZ.js","revision":"efd9738ae5cb650d"},{"url":"/assets/index-DWDerGie.css","revision":"c1127693f77146ba"},{"url":"/assets/LoginPage-CCyCoYLC.js","revision":"f65158c03d3435c3"},{"url":"/assets/McpBoard-Bdy1eh1y.js","revision":"79e0a40c629463e5"},{"url":"/assets/NotFoundView-Bf7U0SXj.js","revision":"ad18a3b822017306"},{"url":"/assets/OnboardingWizard-Gp41ITbG.js","revision":"ca98d38feb7f7a1a"},{"url":"/assets/rolldown-runtime-BUR9erT_.js","revision":"66bc2dc82ec39095"},{"url":"/assets/SchedulerBoard-D818agvn.js","revision":"274ff6a601801756"},{"url":"/assets/shiki-core-BZGQo0dP.js","revision":"855d00a071c225de"},{"url":"/assets/shiki-lang-js-KbSxL_Iu.js","revision":"e81d53e8da7ecf78"},{"url":"/assets/shiki-lang-tools-BNhm00f6.js","revision":"ba6200b72fd342ee"},{"url":"/assets/shiki-lang-web-CTkMxnFF.js","revision":"68fbe9995ce02ec1"},{"url":"/assets/shiki-themes-_UjgcaiI.js","revision":"6fbd6a859ed63c33"},{"url":"/assets/SigmaCanvas-ClJWWgzQ.js","revision":"c622f7ee1f82ad85"},{"url":"/assets/SigmaCanvas-gwv2EvJr.css","revision":"20e692f226ef2093"},{"url":"/assets/SkillsBoard-oUDkuM5d.js","revision":"812edc5632e80f9f"},{"url":"/assets/sourceExplorerControls-BFvIgvZN.js","revision":"1e0df4a5b3d8630c"},{"url":"/assets/useQuery-DI3ai9fH.js","revision":"681fac2720967e20"},{"url":"/assets/useTranslation-cRrdDnLA.js","revision":"33bf2b2a6733277f"},{"url":"/favicon.svg","revision":"7b17af2ead77936a"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"3a814571ad551c73"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"c32d08b35f2cc4e2"},{"url":"/pwa-512.png","revision":"07720c285ef365f3"},{"url":"/pwa-maskable-512.png","revision":"33eae128615a06b6"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
