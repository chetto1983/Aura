const CACHE_NAME="aura-precache-45067d512c122ba7";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"5508e1ffcdd6c2eb"},{"url":"/assets/AppShell-DDCU8non.js","revision":"ef7d98c2d9928b6c"},{"url":"/assets/assistant-ui-D7JgFyK7.js","revision":"a123bba29a8c38f8"},{"url":"/assets/authConfig-zx2TOjGd.js","revision":"05ffa0e6f0a9caf5"},{"url":"/assets/ExternalStoreChat-zm9oJ0w-.js","revision":"78b7b933b5cbdc84"},{"url":"/assets/focusTrap-jgce-j-_.js","revision":"287f642e95c7971f"},{"url":"/assets/governanceApi-Bg5ZNgUS.js","revision":"3b9ed29cd8d54936"},{"url":"/assets/GovernanceWorkspace-Dcj-PsRX.js","revision":"4e3a2b771ca4df4f"},{"url":"/assets/GraphExplorer-n-Tt4ll_.js","revision":"771611ffee2644a3"},{"url":"/assets/index-CEgrgDsT.css","revision":"087fde34ab50f329"},{"url":"/assets/index-DQdemYmc.js","revision":"1a73838d9e19bf24"},{"url":"/assets/json-7kOhAfOp.js","revision":"6363726edc6a73e0"},{"url":"/assets/LoginPage-CYiPg4SY.js","revision":"36c1ccbebb3d6bc8"},{"url":"/assets/McpBoard-BKlv-PWC.js","revision":"b6556fdbb1f42e14"},{"url":"/assets/NotFoundView-BFfIQ4cG.js","revision":"af29b0d665c40efe"},{"url":"/assets/OnboardingWizard-C2X8ZfkH.js","revision":"9487cb6e51a250b7"},{"url":"/assets/rolldown-runtime-BUR9erT_.js","revision":"66bc2dc82ec39095"},{"url":"/assets/SchedulerBoard-pVC16oqS.js","revision":"7f92ebdba2ecfaab"},{"url":"/assets/shiki-core-BZGQo0dP.js","revision":"855d00a071c225de"},{"url":"/assets/shiki-lang-js-KbSxL_Iu.js","revision":"e81d53e8da7ecf78"},{"url":"/assets/shiki-lang-tools-BNhm00f6.js","revision":"ba6200b72fd342ee"},{"url":"/assets/shiki-lang-web-CTkMxnFF.js","revision":"68fbe9995ce02ec1"},{"url":"/assets/shiki-themes-_UjgcaiI.js","revision":"6fbd6a859ed63c33"},{"url":"/assets/SigmaCanvas-CfbTsgOl.js","revision":"e053d57b6dedab7f"},{"url":"/assets/SigmaCanvas-gwv2EvJr.css","revision":"20e692f226ef2093"},{"url":"/assets/SkillsBoard-BgXkiyeu.js","revision":"32e60ce6bafd8656"},{"url":"/assets/sourceExplorerControls-BFvIgvZN.js","revision":"1e0df4a5b3d8630c"},{"url":"/assets/Spinner-BKaiWXvd.js","revision":"186512ff859eef42"},{"url":"/assets/useMutation-CN0oxWvb.js","revision":"6e1ba1059010e761"},{"url":"/assets/useQuery-DpVSLrby.js","revision":"477eb60c902b8692"},{"url":"/assets/useTranslation-XQ1T_JEg.js","revision":"406a7101e4835614"},{"url":"/favicon.svg","revision":"7b17af2ead77936a"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"3a814571ad551c73"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"c32d08b35f2cc4e2"},{"url":"/pwa-512.png","revision":"07720c285ef365f3"},{"url":"/pwa-maskable-512.png","revision":"33eae128615a06b6"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
