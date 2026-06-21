const CACHE_NAME="aura-precache-239f12c780242d9f";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"5508e1ffcdd6c2eb"},{"url":"/assets/AppShell-DJp84HWV.js","revision":"ee1083ff75c32d37"},{"url":"/assets/assistant-ui-D7JgFyK7.js","revision":"a123bba29a8c38f8"},{"url":"/assets/authConfig-CHJJ-V6I.js","revision":"02cd1035c7a302be"},{"url":"/assets/ExternalStoreChat-CnX3Puj9.js","revision":"3094429b09a05d1a"},{"url":"/assets/focusTrap-Cl0Cfr23.js","revision":"d2abb11262dca3f5"},{"url":"/assets/governanceApi-Co69nsCD.js","revision":"c27ac8b2fbc9ea2e"},{"url":"/assets/GovernanceWorkspace-Dj4GEreW.js","revision":"419f5bfb567cf833"},{"url":"/assets/GraphExplorer-BidkH5Dm.js","revision":"39a54bf8a4d58cfa"},{"url":"/assets/index-BZOAse5u.css","revision":"ca7f8a5781c88817"},{"url":"/assets/index-DIqxGTEX.js","revision":"4f47cf7982cd6d4b"},{"url":"/assets/json-7kOhAfOp.js","revision":"6363726edc6a73e0"},{"url":"/assets/LoginPage-DvyK1MIX.js","revision":"86e72246d107397e"},{"url":"/assets/McpBoard--GECXk75.js","revision":"49ad78d737d12a34"},{"url":"/assets/NotFoundView-CraULf93.js","revision":"f3082926102c7be4"},{"url":"/assets/OnboardingWizard-rWgV4Wwb.js","revision":"6f13159d338f3b41"},{"url":"/assets/rolldown-runtime-BUR9erT_.js","revision":"66bc2dc82ec39095"},{"url":"/assets/SchedulerBoard-jilYFCML.js","revision":"76b939a51c257ebf"},{"url":"/assets/shiki-core-BZGQo0dP.js","revision":"855d00a071c225de"},{"url":"/assets/shiki-lang-js-KbSxL_Iu.js","revision":"e81d53e8da7ecf78"},{"url":"/assets/shiki-lang-tools-BNhm00f6.js","revision":"ba6200b72fd342ee"},{"url":"/assets/shiki-lang-web-CTkMxnFF.js","revision":"68fbe9995ce02ec1"},{"url":"/assets/shiki-themes-_UjgcaiI.js","revision":"6fbd6a859ed63c33"},{"url":"/assets/SigmaCanvas-gwv2EvJr.css","revision":"20e692f226ef2093"},{"url":"/assets/SigmaCanvas-WEvySLyV.js","revision":"61e4aeb9b2d0fd73"},{"url":"/assets/SkillsBoard-t5xGv0Y4.js","revision":"8837abbb319503f0"},{"url":"/assets/sourceExplorerControls-BFvIgvZN.js","revision":"1e0df4a5b3d8630c"},{"url":"/assets/useMutation-DyIUzsl3.js","revision":"252b118d8e1dd562"},{"url":"/assets/useQuery-wO1H5bpc.js","revision":"67b90b6ecbc18939"},{"url":"/assets/useTranslation-Ba7YH2Ih.js","revision":"0a62109eeabb2576"},{"url":"/favicon.svg","revision":"7b17af2ead77936a"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"3a814571ad551c73"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"c32d08b35f2cc4e2"},{"url":"/pwa-512.png","revision":"07720c285ef365f3"},{"url":"/pwa-maskable-512.png","revision":"33eae128615a06b6"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
