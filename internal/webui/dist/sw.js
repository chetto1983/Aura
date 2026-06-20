const CACHE_NAME="aura-precache-24e56a2f36f8a783";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"5508e1ffcdd6c2eb"},{"url":"/assets/AppShell-D1aeHEfd.js","revision":"7803b4e0755d0269"},{"url":"/assets/assistant-ui-D7JgFyK7.js","revision":"a123bba29a8c38f8"},{"url":"/assets/authConfig-B3yu2oM2.js","revision":"3611d266fc820869"},{"url":"/assets/ExternalStoreChat-Ekpo6Xge.js","revision":"d00d8bda3284b099"},{"url":"/assets/focusTrap-DU6zHITH.js","revision":"86d5c08c0d6392a2"},{"url":"/assets/governanceApi-Dzib0vHR.js","revision":"666988fbd2c1c0fd"},{"url":"/assets/GovernanceWorkspace-Dz7skjbk.js","revision":"d96da72dc4470238"},{"url":"/assets/GraphExplorer-ysFqb_La.js","revision":"36a5d75820bec2f2"},{"url":"/assets/index-CBExIPMv.js","revision":"fea87375e2ad3935"},{"url":"/assets/index-DWDerGie.css","revision":"c1127693f77146ba"},{"url":"/assets/LoginPage-DNlpMxBq.js","revision":"5594317fa38cf081"},{"url":"/assets/McpBoard-Dni5d6fd.js","revision":"d4878d03261bb401"},{"url":"/assets/NotFoundView-DqNM1K96.js","revision":"53bd03805588e605"},{"url":"/assets/OnboardingWizard-Cl-lH1aP.js","revision":"760dfea0de9afd5b"},{"url":"/assets/rolldown-runtime-BUR9erT_.js","revision":"66bc2dc82ec39095"},{"url":"/assets/SchedulerBoard-DqhPZiR9.js","revision":"2b077ac82ed90f95"},{"url":"/assets/shiki-core-BZGQo0dP.js","revision":"855d00a071c225de"},{"url":"/assets/shiki-lang-js-KbSxL_Iu.js","revision":"e81d53e8da7ecf78"},{"url":"/assets/shiki-lang-tools-BNhm00f6.js","revision":"ba6200b72fd342ee"},{"url":"/assets/shiki-lang-web-CTkMxnFF.js","revision":"68fbe9995ce02ec1"},{"url":"/assets/shiki-themes-_UjgcaiI.js","revision":"6fbd6a859ed63c33"},{"url":"/assets/SigmaCanvas-B31RuOXr.js","revision":"0e7af57f0a03a768"},{"url":"/assets/SigmaCanvas-gwv2EvJr.css","revision":"20e692f226ef2093"},{"url":"/assets/SkillsBoard-D_WLRN7h.js","revision":"ac37f3d272797c8e"},{"url":"/assets/sourceExplorerControls-BFvIgvZN.js","revision":"1e0df4a5b3d8630c"},{"url":"/assets/useQuery-CAMBvzc2.js","revision":"e99aa4f580b81643"},{"url":"/assets/useTranslation-Dop56g3B.js","revision":"334a399375474085"},{"url":"/favicon.svg","revision":"7b17af2ead77936a"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"3a814571ad551c73"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"c32d08b35f2cc4e2"},{"url":"/pwa-512.png","revision":"07720c285ef365f3"},{"url":"/pwa-maskable-512.png","revision":"33eae128615a06b6"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
