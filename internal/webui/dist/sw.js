const CACHE_NAME="aura-precache-0caed1b98c161a42";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"5508e1ffcdd6c2eb"},{"url":"/assets/AppShell-BJvcdvaq.js","revision":"9135bd5c67171dc7"},{"url":"/assets/assistant-ui-D7JgFyK7.js","revision":"a123bba29a8c38f8"},{"url":"/assets/authConfig-EmKXk4BU.js","revision":"991c6a1a56a5b7ee"},{"url":"/assets/ExternalStoreChat-CwpV3kl_.js","revision":"ad06cbfa53f7c0e5"},{"url":"/assets/focusTrap-BaI99Nm3.js","revision":"203128f3de118bcc"},{"url":"/assets/governanceApi-tvqb71-U.js","revision":"cf4b6759a9052b33"},{"url":"/assets/GovernanceWorkspace-Sdk4jJjC.js","revision":"81150edc38e3b195"},{"url":"/assets/GraphExplorer-ChtXDB-c.js","revision":"4e04649c28939d6f"},{"url":"/assets/index-BSBiWVpo.css","revision":"635610aa590a0bae"},{"url":"/assets/index-CvaoK2gk.js","revision":"35139a5ac34096b2"},{"url":"/assets/json-7kOhAfOp.js","revision":"6363726edc6a73e0"},{"url":"/assets/LoginPage-qeomVJ4i.js","revision":"230b976fcb6ad3a7"},{"url":"/assets/McpBoard-DaWY5qGS.js","revision":"5e46e965ded00fcb"},{"url":"/assets/NotFoundView-D9X7-frr.js","revision":"4c96368096832623"},{"url":"/assets/OnboardingWizard-BmB_V3O7.js","revision":"9a4b493e646d04e3"},{"url":"/assets/rolldown-runtime-BUR9erT_.js","revision":"66bc2dc82ec39095"},{"url":"/assets/SchedulerBoard-BtT6ivrr.js","revision":"849f69fdc6b8b568"},{"url":"/assets/shiki-core-BZGQo0dP.js","revision":"855d00a071c225de"},{"url":"/assets/shiki-lang-js-KbSxL_Iu.js","revision":"e81d53e8da7ecf78"},{"url":"/assets/shiki-lang-tools-BNhm00f6.js","revision":"ba6200b72fd342ee"},{"url":"/assets/shiki-lang-web-CTkMxnFF.js","revision":"68fbe9995ce02ec1"},{"url":"/assets/shiki-themes-_UjgcaiI.js","revision":"6fbd6a859ed63c33"},{"url":"/assets/SigmaCanvas-DwhmKj-c.js","revision":"ffc64385f053e425"},{"url":"/assets/SigmaCanvas-gwv2EvJr.css","revision":"20e692f226ef2093"},{"url":"/assets/SkillsBoard-DB-QfCVn.js","revision":"f9e23cda275a7e71"},{"url":"/assets/sourceExplorerControls-BFvIgvZN.js","revision":"1e0df4a5b3d8630c"},{"url":"/assets/useQuery-DZZAtCUL.js","revision":"3d0656738949aad9"},{"url":"/assets/useTranslation-C7ZEPwLh.js","revision":"6a7c9085cf32899b"},{"url":"/favicon.svg","revision":"7b17af2ead77936a"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"3a814571ad551c73"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"c32d08b35f2cc4e2"},{"url":"/pwa-512.png","revision":"07720c285ef365f3"},{"url":"/pwa-maskable-512.png","revision":"33eae128615a06b6"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
