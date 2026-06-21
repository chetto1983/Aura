const CACHE_NAME="aura-precache-68040e6efb9706c7";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"5508e1ffcdd6c2eb"},{"url":"/assets/AppShell-D5UR-76p.js","revision":"3ac0cec43c51b1c6"},{"url":"/assets/assistant-ui-D7JgFyK7.js","revision":"a123bba29a8c38f8"},{"url":"/assets/authConfig-CoMI1WrI.js","revision":"5d85c1bb6ed03558"},{"url":"/assets/ExternalStoreChat-DHU9esMx.js","revision":"ae24f35c56b1aaff"},{"url":"/assets/focusTrap-CURdNO6u.js","revision":"e20b953539cfbd45"},{"url":"/assets/governanceApi-B6ws53s3.js","revision":"6e70dee892b0d3dc"},{"url":"/assets/GovernanceWorkspace-Ct_I8dVT.js","revision":"772428fe12a34bc5"},{"url":"/assets/GraphExplorer-COFUhdko.js","revision":"8f348843eb1d8fe1"},{"url":"/assets/index-aeYJ-P8j.css","revision":"98387d03fafd25b9"},{"url":"/assets/index-f_90BJh2.js","revision":"ba4bccd845cbd0bb"},{"url":"/assets/json-7kOhAfOp.js","revision":"6363726edc6a73e0"},{"url":"/assets/LoginPage-DY3uE5NO.js","revision":"90672ee6c1bee259"},{"url":"/assets/McpBoard-H4Pa3OL6.js","revision":"8daffc3bbe9c9df2"},{"url":"/assets/NotFoundView-C0C_RLiH.js","revision":"cd97bd04f454983a"},{"url":"/assets/OnboardingWizard-CrVyaXQW.js","revision":"2aa6a5c2d0542674"},{"url":"/assets/rolldown-runtime-BUR9erT_.js","revision":"66bc2dc82ec39095"},{"url":"/assets/SchedulerBoard-Dl-mPXl4.js","revision":"fe5aa0d02f5fae60"},{"url":"/assets/shiki-core-BZGQo0dP.js","revision":"855d00a071c225de"},{"url":"/assets/shiki-lang-js-KbSxL_Iu.js","revision":"e81d53e8da7ecf78"},{"url":"/assets/shiki-lang-tools-BNhm00f6.js","revision":"ba6200b72fd342ee"},{"url":"/assets/shiki-lang-web-CTkMxnFF.js","revision":"68fbe9995ce02ec1"},{"url":"/assets/shiki-themes-_UjgcaiI.js","revision":"6fbd6a859ed63c33"},{"url":"/assets/SigmaCanvas-Cv2nZjG6.js","revision":"10bcf54fe44d018a"},{"url":"/assets/SigmaCanvas-gwv2EvJr.css","revision":"20e692f226ef2093"},{"url":"/assets/SkillsBoard-B57-5DN3.js","revision":"e18a968a9a1a3fae"},{"url":"/assets/sourceExplorerControls-BFvIgvZN.js","revision":"1e0df4a5b3d8630c"},{"url":"/assets/Spinner-CiXxrawa.js","revision":"bd8d5ca86a673b76"},{"url":"/assets/useMutation-BB0w2ETr.js","revision":"2876b5472825982a"},{"url":"/assets/useQuery-An7SBxjC.js","revision":"f846fa06f861b944"},{"url":"/assets/useTranslation-ZdKqodMe.js","revision":"53e3239bf3078e77"},{"url":"/favicon.svg","revision":"7b17af2ead77936a"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"3a814571ad551c73"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"c32d08b35f2cc4e2"},{"url":"/pwa-512.png","revision":"07720c285ef365f3"},{"url":"/pwa-maskable-512.png","revision":"33eae128615a06b6"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
