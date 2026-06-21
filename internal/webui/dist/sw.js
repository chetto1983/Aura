const CACHE_NAME="aura-precache-a4b623bb5de9739b";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"5508e1ffcdd6c2eb"},{"url":"/assets/AppShell-CxQX0Jck.js","revision":"48e8748e7c125ed3"},{"url":"/assets/assistant-ui-D7JgFyK7.js","revision":"a123bba29a8c38f8"},{"url":"/assets/authConfig-CcPRMYjI.js","revision":"f62e5b156d96ead8"},{"url":"/assets/ExternalStoreChat-BglbL8rC.js","revision":"5cceba80877bbeb7"},{"url":"/assets/focusTrap-KIXEEcSp.js","revision":"3f7b4a1eed25008d"},{"url":"/assets/governanceApi-9FVgCBzI.js","revision":"d68fd2cd851d6413"},{"url":"/assets/GovernanceWorkspace-BU5DtoOq.js","revision":"03617b4a7cc9efd6"},{"url":"/assets/GraphExplorer-CN91qSXl.js","revision":"3ca63e30c95eb65b"},{"url":"/assets/index-BfkzGcZC.css","revision":"6d02c5c98e3900cf"},{"url":"/assets/index-DqnYjNvJ.js","revision":"5770a296e3084166"},{"url":"/assets/json-7kOhAfOp.js","revision":"6363726edc6a73e0"},{"url":"/assets/LoginPage-D2NmyxxE.js","revision":"2defd6a495dc67c7"},{"url":"/assets/McpBoard-B2tGuK2A.js","revision":"4736e7c315e84525"},{"url":"/assets/NotFoundView-B4URtb0H.js","revision":"ec20af02631d14ab"},{"url":"/assets/OnboardingWizard-DsnUE5rv.js","revision":"80e50dd6fddeeb7e"},{"url":"/assets/rolldown-runtime-BUR9erT_.js","revision":"66bc2dc82ec39095"},{"url":"/assets/SchedulerBoard-Bo8xTo4L.js","revision":"5b3dc49b7996f87c"},{"url":"/assets/shiki-core-BZGQo0dP.js","revision":"855d00a071c225de"},{"url":"/assets/shiki-lang-js-KbSxL_Iu.js","revision":"e81d53e8da7ecf78"},{"url":"/assets/shiki-lang-tools-BNhm00f6.js","revision":"ba6200b72fd342ee"},{"url":"/assets/shiki-lang-web-CTkMxnFF.js","revision":"68fbe9995ce02ec1"},{"url":"/assets/shiki-themes-_UjgcaiI.js","revision":"6fbd6a859ed63c33"},{"url":"/assets/SigmaCanvas-DDXEQ7Ni.js","revision":"b353adddfa2f061e"},{"url":"/assets/SigmaCanvas-gwv2EvJr.css","revision":"20e692f226ef2093"},{"url":"/assets/SkillsBoard-B-THR5Fy.js","revision":"6223a13222e6cadf"},{"url":"/assets/sourceExplorerControls-BFvIgvZN.js","revision":"1e0df4a5b3d8630c"},{"url":"/assets/Spinner-B411osg3.js","revision":"9811e1c72d8e5e9f"},{"url":"/assets/useMutation-0gdsWwYC.js","revision":"d98ee2f4b95d1ed0"},{"url":"/assets/useQuery-DcTJh3PP.js","revision":"deafbdaee8c0b2fe"},{"url":"/assets/useTranslation-DI_XPGju.js","revision":"183f39656fa142ff"},{"url":"/favicon.svg","revision":"7b17af2ead77936a"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"3a814571ad551c73"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"c32d08b35f2cc4e2"},{"url":"/pwa-512.png","revision":"07720c285ef365f3"},{"url":"/pwa-maskable-512.png","revision":"33eae128615a06b6"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
