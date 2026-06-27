const CACHE_NAME="aura-precache-603901bb4651ed43";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"5508e1ffcdd6c2eb"},{"url":"/assets/AppShell-DOFDsgJY.js","revision":"8fd3dcf581b4dfc7"},{"url":"/assets/assistant-ui-D7JgFyK7.js","revision":"a123bba29a8c38f8"},{"url":"/assets/authConfig-KBF9A-_3.js","revision":"22736b13b5db14a1"},{"url":"/assets/ExternalStoreChat-DRPtUzeS.js","revision":"30b58d4eea15a6d2"},{"url":"/assets/focusTrap-rSZhTaBe.js","revision":"6b711a37c56860c1"},{"url":"/assets/governanceApi-P1rJjHsr.js","revision":"18d3450e0df7b595"},{"url":"/assets/GovernanceWorkspace-CYkG7HOt.js","revision":"32c188299ef414db"},{"url":"/assets/GraphExplorer-BGWHvDuL.js","revision":"3e0cf3b49b332ab3"},{"url":"/assets/index-3Gky3c0e.js","revision":"5eab709d09190703"},{"url":"/assets/index-CEgrgDsT.css","revision":"087fde34ab50f329"},{"url":"/assets/json-7kOhAfOp.js","revision":"6363726edc6a73e0"},{"url":"/assets/LoginPage-v-bbLfHQ.js","revision":"c5c158105e911c4c"},{"url":"/assets/McpBoard-Co9fRnJP.js","revision":"b1ff319292319930"},{"url":"/assets/NotFoundView-aFpdr9qs.js","revision":"bd3dedc0f671f093"},{"url":"/assets/OnboardingWizard-ILrmAhwA.js","revision":"b6aab4f9a05b0389"},{"url":"/assets/rolldown-runtime-BUR9erT_.js","revision":"66bc2dc82ec39095"},{"url":"/assets/SchedulerBoard-CRcfq0_3.js","revision":"fff4739c06af639e"},{"url":"/assets/shiki-core-BZGQo0dP.js","revision":"855d00a071c225de"},{"url":"/assets/shiki-lang-js-KbSxL_Iu.js","revision":"e81d53e8da7ecf78"},{"url":"/assets/shiki-lang-tools-BNhm00f6.js","revision":"ba6200b72fd342ee"},{"url":"/assets/shiki-lang-web-CTkMxnFF.js","revision":"68fbe9995ce02ec1"},{"url":"/assets/shiki-themes-_UjgcaiI.js","revision":"6fbd6a859ed63c33"},{"url":"/assets/SigmaCanvas-CnOshd79.js","revision":"529cc663a34cccb9"},{"url":"/assets/SigmaCanvas-gwv2EvJr.css","revision":"20e692f226ef2093"},{"url":"/assets/SkillsBoard-nvgfDUhz.js","revision":"1594cbdee7df89d6"},{"url":"/assets/sourceExplorerControls-BFvIgvZN.js","revision":"1e0df4a5b3d8630c"},{"url":"/assets/Spinner-BduTu93I.js","revision":"dfe7af9a8b868617"},{"url":"/assets/useMutation-Dt3Lm5f9.js","revision":"5b7f5a4eb6dec2e3"},{"url":"/assets/useQuery-CjHgl5nh.js","revision":"68dc03fc139c9fea"},{"url":"/assets/useTranslation-eba1n3_2.js","revision":"736cc80815635190"},{"url":"/favicon.svg","revision":"7b17af2ead77936a"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"3a814571ad551c73"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"c32d08b35f2cc4e2"},{"url":"/pwa-512.png","revision":"07720c285ef365f3"},{"url":"/pwa-maskable-512.png","revision":"33eae128615a06b6"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
