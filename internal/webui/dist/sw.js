const CACHE_NAME="aura-precache-e468c28bad4a2a03";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"5508e1ffcdd6c2eb"},{"url":"/assets/AppShell-CjJVyO8Y.js","revision":"1ceae86d386876f3"},{"url":"/assets/assistant-ui-D7JgFyK7.js","revision":"a123bba29a8c38f8"},{"url":"/assets/authConfig-DRHMlJKY.js","revision":"b948db8ae2fc3a61"},{"url":"/assets/ExternalStoreChat-ca6nLSqa.js","revision":"606203d966f9fcb3"},{"url":"/assets/focusTrap-CHOs5mfW.js","revision":"265ab7bd758d2b87"},{"url":"/assets/governanceApi-DsGprD80.js","revision":"53847a4671d84ff9"},{"url":"/assets/GovernanceWorkspace-B0jEnjFR.js","revision":"5f106686b45ef689"},{"url":"/assets/GraphExplorer-CkuH7OLN.js","revision":"974c1796f7a2f2ec"},{"url":"/assets/index-CGwIyNCQ.js","revision":"5adfc7e26b61b94f"},{"url":"/assets/index-dStc5Ehh.css","revision":"810e1cb903174025"},{"url":"/assets/json-7kOhAfOp.js","revision":"6363726edc6a73e0"},{"url":"/assets/LoginPage-BsqYRlue.js","revision":"8249d6f4dda1fabb"},{"url":"/assets/McpBoard-ClBaWyXV.js","revision":"b78ad15d40a41852"},{"url":"/assets/NotFoundView-DOwKEHT8.js","revision":"fb407cbc0a044d64"},{"url":"/assets/OnboardingWizard-NVh_ll5B.js","revision":"46a27663653087b2"},{"url":"/assets/rolldown-runtime-BUR9erT_.js","revision":"66bc2dc82ec39095"},{"url":"/assets/SchedulerBoard-BzNGimr3.js","revision":"58869f98539591c3"},{"url":"/assets/shiki-core-BZGQo0dP.js","revision":"855d00a071c225de"},{"url":"/assets/shiki-lang-js-KbSxL_Iu.js","revision":"e81d53e8da7ecf78"},{"url":"/assets/shiki-lang-tools-BNhm00f6.js","revision":"ba6200b72fd342ee"},{"url":"/assets/shiki-lang-web-CTkMxnFF.js","revision":"68fbe9995ce02ec1"},{"url":"/assets/shiki-themes-_UjgcaiI.js","revision":"6fbd6a859ed63c33"},{"url":"/assets/SigmaCanvas-DQUw-Mst.js","revision":"b9fd17f52dcd1a40"},{"url":"/assets/SigmaCanvas-gwv2EvJr.css","revision":"20e692f226ef2093"},{"url":"/assets/SkillsBoard-waH8i-DC.js","revision":"52e9f9294835ace3"},{"url":"/assets/sourceExplorerControls-BFvIgvZN.js","revision":"1e0df4a5b3d8630c"},{"url":"/assets/Spinner-Djlw1Snn.js","revision":"31fafd5cf409b1af"},{"url":"/assets/useMutation-B1z6PB9v.js","revision":"234ee1bf04106e18"},{"url":"/assets/useQuery-DU2JqC3x.js","revision":"08cab43b43f764af"},{"url":"/assets/useTranslation-BuECG7sC.js","revision":"7c0c32668c68209a"},{"url":"/favicon.svg","revision":"7b17af2ead77936a"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"3a814571ad551c73"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"c32d08b35f2cc4e2"},{"url":"/pwa-512.png","revision":"07720c285ef365f3"},{"url":"/pwa-maskable-512.png","revision":"33eae128615a06b6"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
