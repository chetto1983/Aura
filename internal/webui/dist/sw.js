const CACHE_NAME="aura-precache-04b667245c64226d";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"5508e1ffcdd6c2eb"},{"url":"/assets/AppShell-C2plWR5O.js","revision":"99107f7ecf2549f0"},{"url":"/assets/assistant-ui-D7JgFyK7.js","revision":"a123bba29a8c38f8"},{"url":"/assets/authConfig-CjQp4AeH.js","revision":"dff5a42849e8c6b7"},{"url":"/assets/ExternalStoreChat-ChmbwReU.js","revision":"5df7649aff6e30c3"},{"url":"/assets/focusTrap-CvQm5KgY.js","revision":"c388e1fbc2f09696"},{"url":"/assets/governanceApi-Dk7HzBOx.js","revision":"26cf470975431c59"},{"url":"/assets/GovernanceWorkspace-C5taMzWD.js","revision":"fc320faeb11a9762"},{"url":"/assets/GraphExplorer-ClYPsYOt.js","revision":"db184d6fcca1630e"},{"url":"/assets/index-DOFXcSH4.js","revision":"71f9cb0908675592"},{"url":"/assets/index-dStc5Ehh.css","revision":"810e1cb903174025"},{"url":"/assets/json-7kOhAfOp.js","revision":"6363726edc6a73e0"},{"url":"/assets/LoginPage-CF7u-1WI.js","revision":"21f1d93021e35720"},{"url":"/assets/McpBoard-HPFLjlY3.js","revision":"b1271a720c957c35"},{"url":"/assets/NotFoundView-DK_ewaDh.js","revision":"2a06f770553763a8"},{"url":"/assets/OnboardingWizard-BCn_7QAK.js","revision":"67d4f9ca6bcf8015"},{"url":"/assets/rolldown-runtime-BUR9erT_.js","revision":"66bc2dc82ec39095"},{"url":"/assets/SchedulerBoard-DN94k6t3.js","revision":"6e034f136e189bfc"},{"url":"/assets/shiki-core-BZGQo0dP.js","revision":"855d00a071c225de"},{"url":"/assets/shiki-lang-js-KbSxL_Iu.js","revision":"e81d53e8da7ecf78"},{"url":"/assets/shiki-lang-tools-BNhm00f6.js","revision":"ba6200b72fd342ee"},{"url":"/assets/shiki-lang-web-CTkMxnFF.js","revision":"68fbe9995ce02ec1"},{"url":"/assets/shiki-themes-_UjgcaiI.js","revision":"6fbd6a859ed63c33"},{"url":"/assets/SigmaCanvas-BWJFodNB.js","revision":"4ab826fe7d8e0041"},{"url":"/assets/SigmaCanvas-gwv2EvJr.css","revision":"20e692f226ef2093"},{"url":"/assets/SkillsBoard-DtRwFpX6.js","revision":"3c9edd012aa0284b"},{"url":"/assets/sourceExplorerControls-BFvIgvZN.js","revision":"1e0df4a5b3d8630c"},{"url":"/assets/Spinner-CLV-mIzJ.js","revision":"d11286e9ebd8fe30"},{"url":"/assets/useMutation-DMJ5Zla6.js","revision":"33a73ad99186c110"},{"url":"/assets/useQuery-CMcp0Kmy.js","revision":"8a2667e13ebfd3d4"},{"url":"/assets/useTranslation-D9b9V3r9.js","revision":"67f95f1d6075f1ec"},{"url":"/favicon.svg","revision":"7b17af2ead77936a"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"3a814571ad551c73"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"c32d08b35f2cc4e2"},{"url":"/pwa-512.png","revision":"07720c285ef365f3"},{"url":"/pwa-maskable-512.png","revision":"33eae128615a06b6"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
