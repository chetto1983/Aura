const CACHE_NAME="aura-precache-9fc2c71af65c29b4";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"5508e1ffcdd6c2eb"},{"url":"/assets/AppShell-BWZyBBd9.js","revision":"80f846b6e9e760f8"},{"url":"/assets/assistant-ui-D7JgFyK7.js","revision":"a123bba29a8c38f8"},{"url":"/assets/authConfig-DLZzgCSv.js","revision":"6efb03bee79b0e2b"},{"url":"/assets/ExternalStoreChat-CKW8rP_4.js","revision":"d821583da9e51b55"},{"url":"/assets/focusTrap-CcNhwgyQ.js","revision":"9024c7e981328e09"},{"url":"/assets/governanceApi-Cb4vbAIv.js","revision":"b160fbe187d3c76d"},{"url":"/assets/GovernanceWorkspace-hXZMXlnT.js","revision":"3572ddb43d0c95f5"},{"url":"/assets/GraphExplorer-uecrUXcc.js","revision":"108b66d3bcd3985a"},{"url":"/assets/index-DWDerGie.css","revision":"c1127693f77146ba"},{"url":"/assets/index-OM07cuwA.js","revision":"d8a5849563ee80c0"},{"url":"/assets/LoginPage-C3e6sYus.js","revision":"f097daf6285ec997"},{"url":"/assets/McpBoard-Bd4x6s3v.js","revision":"a240f44a7c69f8ab"},{"url":"/assets/NotFoundView-DfBNKnK8.js","revision":"982d885cf829a967"},{"url":"/assets/OnboardingWizard-BDluN6lM.js","revision":"d976e1265fccc40c"},{"url":"/assets/rolldown-runtime-BUR9erT_.js","revision":"66bc2dc82ec39095"},{"url":"/assets/SchedulerBoard-YEl_W-g7.js","revision":"57ec1a1386364186"},{"url":"/assets/shiki-core-BZGQo0dP.js","revision":"855d00a071c225de"},{"url":"/assets/shiki-lang-js-KbSxL_Iu.js","revision":"e81d53e8da7ecf78"},{"url":"/assets/shiki-lang-tools-BNhm00f6.js","revision":"ba6200b72fd342ee"},{"url":"/assets/shiki-lang-web-CTkMxnFF.js","revision":"68fbe9995ce02ec1"},{"url":"/assets/shiki-themes-_UjgcaiI.js","revision":"6fbd6a859ed63c33"},{"url":"/assets/SigmaCanvas-C6hYdhJn.js","revision":"604d46ae8d181069"},{"url":"/assets/SigmaCanvas-gwv2EvJr.css","revision":"20e692f226ef2093"},{"url":"/assets/SkillsBoard-BMvc8iHA.js","revision":"21a9a3ae1bfa5fb1"},{"url":"/assets/sourceExplorerControls-BFvIgvZN.js","revision":"1e0df4a5b3d8630c"},{"url":"/assets/useQuery-Duc6wkEf.js","revision":"a2e1a1fad6959c10"},{"url":"/assets/useTranslation-CTWYQFnu.js","revision":"07185dbf3706ac42"},{"url":"/favicon.svg","revision":"7b17af2ead77936a"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"3a814571ad551c73"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"c32d08b35f2cc4e2"},{"url":"/pwa-512.png","revision":"07720c285ef365f3"},{"url":"/pwa-maskable-512.png","revision":"33eae128615a06b6"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
