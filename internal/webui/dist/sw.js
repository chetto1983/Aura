const CACHE_NAME="aura-precache-2023f8fbbc855b27";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"5508e1ffcdd6c2eb"},{"url":"/assets/AppShell-pdeIcsca.js","revision":"15baa76ffb6cdcaf"},{"url":"/assets/assistant-ui-D7JgFyK7.js","revision":"a123bba29a8c38f8"},{"url":"/assets/authConfig-rsamR0ld.js","revision":"e550710183d8823f"},{"url":"/assets/ExternalStoreChat-CoVx4Qpd.js","revision":"c154e0029ee81d4b"},{"url":"/assets/focusTrap--nYtSWQw.js","revision":"09fe6a211b497759"},{"url":"/assets/governanceApi-C7ElibeE.js","revision":"b42ddc5c1144e051"},{"url":"/assets/GovernanceWorkspace-DMEEMrF4.js","revision":"7745f95fc87ad895"},{"url":"/assets/GraphExplorer-g6RHzrGo.js","revision":"b29660ec9e50d712"},{"url":"/assets/index-B85FzBoE.js","revision":"e3b1123d1867aa64"},{"url":"/assets/index-vdd3ICZ_.css","revision":"40d4d7598335c95c"},{"url":"/assets/json-7kOhAfOp.js","revision":"6363726edc6a73e0"},{"url":"/assets/LoginPage-DkR1NXl8.js","revision":"b5bc4019d0628ac3"},{"url":"/assets/McpBoard-DcSM2PRF.js","revision":"d42206086a81989b"},{"url":"/assets/NotFoundView-BbutJZny.js","revision":"2daccc7c8c904a00"},{"url":"/assets/OnboardingWizard-D2DAYpfr.js","revision":"2ed7c7fdf865d9e3"},{"url":"/assets/rolldown-runtime-BUR9erT_.js","revision":"66bc2dc82ec39095"},{"url":"/assets/SchedulerBoard-CpxDW48H.js","revision":"17beb51eef0b6acf"},{"url":"/assets/shiki-core-BZGQo0dP.js","revision":"855d00a071c225de"},{"url":"/assets/shiki-lang-js-KbSxL_Iu.js","revision":"e81d53e8da7ecf78"},{"url":"/assets/shiki-lang-tools-BNhm00f6.js","revision":"ba6200b72fd342ee"},{"url":"/assets/shiki-lang-web-CTkMxnFF.js","revision":"68fbe9995ce02ec1"},{"url":"/assets/shiki-themes-_UjgcaiI.js","revision":"6fbd6a859ed63c33"},{"url":"/assets/SigmaCanvas-gwv2EvJr.css","revision":"20e692f226ef2093"},{"url":"/assets/SigmaCanvas-MCjQULJP.js","revision":"b20d817a61e61720"},{"url":"/assets/SkillsBoard-z1hTiaAy.js","revision":"aafe6c426485def3"},{"url":"/assets/sourceExplorerControls-BFvIgvZN.js","revision":"1e0df4a5b3d8630c"},{"url":"/assets/useQuery-B0-WKUfl.js","revision":"69dacbf0cfd761c3"},{"url":"/assets/useTranslation-DKJewvDH.js","revision":"0b4b34400d4eaa70"},{"url":"/favicon.svg","revision":"7b17af2ead77936a"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"3a814571ad551c73"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"c32d08b35f2cc4e2"},{"url":"/pwa-512.png","revision":"07720c285ef365f3"},{"url":"/pwa-maskable-512.png","revision":"33eae128615a06b6"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
