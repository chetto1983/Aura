const CACHE_NAME="aura-precache-580b05a208df1320";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"5508e1ffcdd6c2eb"},{"url":"/assets/AppShell-BDJ2RYtB.js","revision":"a995f87ccdcd5fb7"},{"url":"/assets/assistant-ui-D7JgFyK7.js","revision":"a123bba29a8c38f8"},{"url":"/assets/authConfig-lorFMtiu.js","revision":"48a3462c3c85dd51"},{"url":"/assets/ExternalStoreChat-oEj-BBtD.js","revision":"590de01d50d1df94"},{"url":"/assets/focusTrap-Bsf5JPS1.js","revision":"e7544a074262ac36"},{"url":"/assets/governanceApi-DG8Rtu6U.js","revision":"1a13761cacd6b750"},{"url":"/assets/GovernanceWorkspace-BIhXcKMe.js","revision":"875e1ec50ed96491"},{"url":"/assets/GraphExplorer-CgUet28O.js","revision":"ead04ecdfeaee8d4"},{"url":"/assets/index-D8DG9vaZ.js","revision":"f6e285a676b9bb8f"},{"url":"/assets/index-dStc5Ehh.css","revision":"810e1cb903174025"},{"url":"/assets/json-7kOhAfOp.js","revision":"6363726edc6a73e0"},{"url":"/assets/LoginPage-DpCaaaME.js","revision":"90f28ba41ac100d4"},{"url":"/assets/McpBoard-BfL3aRWq.js","revision":"d8365dbb6a4b64db"},{"url":"/assets/NotFoundView-B4tj8nUF.js","revision":"0cf78895ac427e3c"},{"url":"/assets/OnboardingWizard-hCByP5Tm.js","revision":"e00da42cb80a64c1"},{"url":"/assets/rolldown-runtime-BUR9erT_.js","revision":"66bc2dc82ec39095"},{"url":"/assets/SchedulerBoard-B6gy-nMk.js","revision":"c0c12a6e590a6c53"},{"url":"/assets/shiki-core-BZGQo0dP.js","revision":"855d00a071c225de"},{"url":"/assets/shiki-lang-js-KbSxL_Iu.js","revision":"e81d53e8da7ecf78"},{"url":"/assets/shiki-lang-tools-BNhm00f6.js","revision":"ba6200b72fd342ee"},{"url":"/assets/shiki-lang-web-CTkMxnFF.js","revision":"68fbe9995ce02ec1"},{"url":"/assets/shiki-themes-_UjgcaiI.js","revision":"6fbd6a859ed63c33"},{"url":"/assets/SigmaCanvas-DtLP56cV.js","revision":"b8bd73fe1ea34360"},{"url":"/assets/SigmaCanvas-gwv2EvJr.css","revision":"20e692f226ef2093"},{"url":"/assets/SkillsBoard-D0-6en3O.js","revision":"e26d173e24b361be"},{"url":"/assets/sourceExplorerControls-BFvIgvZN.js","revision":"1e0df4a5b3d8630c"},{"url":"/assets/Spinner-CbPQ_V4W.js","revision":"19333f3c3eb152d6"},{"url":"/assets/useMutation-DnMgN2f3.js","revision":"b27b46beb1473304"},{"url":"/assets/useQuery-CQAxaSmM.js","revision":"29d13a2c8976aa0a"},{"url":"/assets/useTranslation-BSNDTb1r.js","revision":"12b80f71fa75f3cb"},{"url":"/favicon.svg","revision":"7b17af2ead77936a"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"3a814571ad551c73"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"c32d08b35f2cc4e2"},{"url":"/pwa-512.png","revision":"07720c285ef365f3"},{"url":"/pwa-maskable-512.png","revision":"33eae128615a06b6"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
