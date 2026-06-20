const CACHE_NAME="aura-precache-e3ba163939201fcf";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"5508e1ffcdd6c2eb"},{"url":"/assets/AppShell-B-oOuMj0.js","revision":"f346bf651faa3c28"},{"url":"/assets/assistant-ui-D7JgFyK7.js","revision":"a123bba29a8c38f8"},{"url":"/assets/authConfig-DI3HxMsY.js","revision":"d5443c2b13fb2a91"},{"url":"/assets/ExternalStoreChat-Caiager9.js","revision":"b062840d2531bc85"},{"url":"/assets/focusTrap-BMX3Kfoy.js","revision":"7c7fe8b9cc94d0ed"},{"url":"/assets/governanceApi-DBaRIGgr.js","revision":"2fe52ede08105caa"},{"url":"/assets/GovernanceWorkspace-BIyHR_-z.js","revision":"f70c18a4c60ca1c1"},{"url":"/assets/GraphExplorer-DUmMqc1Z.js","revision":"ed934946ecf77b5f"},{"url":"/assets/index-BCWHVBm6.js","revision":"ee79f3bfaa9760c4"},{"url":"/assets/index-BSBiWVpo.css","revision":"635610aa590a0bae"},{"url":"/assets/LoginPage-DpKH3ZKu.js","revision":"94f099c348a79d6f"},{"url":"/assets/McpBoard-BeFvAEQn.js","revision":"e2f57f3dbc9bcaec"},{"url":"/assets/NotFoundView-fZI3_rex.js","revision":"2713f45b2e3a59df"},{"url":"/assets/OnboardingWizard-BhPLNDuV.js","revision":"8ae49979ce28a9d4"},{"url":"/assets/rolldown-runtime-BUR9erT_.js","revision":"66bc2dc82ec39095"},{"url":"/assets/SchedulerBoard-DhQLbiPE.js","revision":"2bea71dd304a46fd"},{"url":"/assets/shiki-core-BZGQo0dP.js","revision":"855d00a071c225de"},{"url":"/assets/shiki-lang-js-KbSxL_Iu.js","revision":"e81d53e8da7ecf78"},{"url":"/assets/shiki-lang-tools-BNhm00f6.js","revision":"ba6200b72fd342ee"},{"url":"/assets/shiki-lang-web-CTkMxnFF.js","revision":"68fbe9995ce02ec1"},{"url":"/assets/shiki-themes-_UjgcaiI.js","revision":"6fbd6a859ed63c33"},{"url":"/assets/SigmaCanvas-BADQVYhu.js","revision":"e5c65a8e35822938"},{"url":"/assets/SigmaCanvas-gwv2EvJr.css","revision":"20e692f226ef2093"},{"url":"/assets/SkillsBoard-adYk0_uX.js","revision":"fb0bec7623021208"},{"url":"/assets/sourceExplorerControls-BFvIgvZN.js","revision":"1e0df4a5b3d8630c"},{"url":"/assets/useQuery-CfF7PO6y.js","revision":"07a628671c95f52a"},{"url":"/assets/useTranslation-DW1rwlRn.js","revision":"860dfbbf88e59ae3"},{"url":"/favicon.svg","revision":"7b17af2ead77936a"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"3a814571ad551c73"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"c32d08b35f2cc4e2"},{"url":"/pwa-512.png","revision":"07720c285ef365f3"},{"url":"/pwa-maskable-512.png","revision":"33eae128615a06b6"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
