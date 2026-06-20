const CACHE_NAME="aura-precache-52202189cc4de19d";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"5508e1ffcdd6c2eb"},{"url":"/assets/AppShell-DyX4mOCg.js","revision":"00bb804251a80e37"},{"url":"/assets/assistant-ui-D7JgFyK7.js","revision":"a123bba29a8c38f8"},{"url":"/assets/authConfig-CEAWRngu.js","revision":"33019b8a3caa82ba"},{"url":"/assets/ExternalStoreChat-BZd_vTuW.js","revision":"8dd40174e3cb9114"},{"url":"/assets/focusTrap-BYhtCpJG.js","revision":"4a327e3d702ba210"},{"url":"/assets/governanceApi-DMnNBkA9.js","revision":"a7f01713e9e2ff8b"},{"url":"/assets/GovernanceWorkspace-C-nUv9jx.js","revision":"4fac391c3019ca00"},{"url":"/assets/GraphExplorer-BPjwthG5.js","revision":"af317c617e72ae3a"},{"url":"/assets/index-BA4R_-KD.js","revision":"929545f2d35aeb97"},{"url":"/assets/index-M8xH7rkD.css","revision":"2b0cf2a443e3151f"},{"url":"/assets/LoginPage-BtTL-h1U.js","revision":"6e0df70c643cdf64"},{"url":"/assets/McpBoard-BcEocjKR.js","revision":"b102255221acfdc7"},{"url":"/assets/NotFoundView-Cup-RBeX.js","revision":"490b0d4c9b0a188e"},{"url":"/assets/rolldown-runtime-BUR9erT_.js","revision":"66bc2dc82ec39095"},{"url":"/assets/SchedulerBoard-DgCDirID.js","revision":"3351d1571c6c9176"},{"url":"/assets/shiki-core-BZGQo0dP.js","revision":"855d00a071c225de"},{"url":"/assets/shiki-lang-js-KbSxL_Iu.js","revision":"e81d53e8da7ecf78"},{"url":"/assets/shiki-lang-tools-BNhm00f6.js","revision":"ba6200b72fd342ee"},{"url":"/assets/shiki-lang-web-CTkMxnFF.js","revision":"68fbe9995ce02ec1"},{"url":"/assets/shiki-themes-_UjgcaiI.js","revision":"6fbd6a859ed63c33"},{"url":"/assets/SigmaCanvas-Bxn6jL4Q.js","revision":"e8465cf01589e117"},{"url":"/assets/SigmaCanvas-gwv2EvJr.css","revision":"20e692f226ef2093"},{"url":"/assets/SkillsBoard-diQ7dUih.js","revision":"caa7e242501864d9"},{"url":"/assets/sourceExplorerControls-BFvIgvZN.js","revision":"1e0df4a5b3d8630c"},{"url":"/assets/useQuery-DjViXjqG.js","revision":"04a2c6639d5ee136"},{"url":"/assets/useTranslation-BTZYqp-1.js","revision":"ae54d1133bbe6fe9"},{"url":"/favicon.svg","revision":"7b17af2ead77936a"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"3a814571ad551c73"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"c32d08b35f2cc4e2"},{"url":"/pwa-512.png","revision":"07720c285ef365f3"},{"url":"/pwa-maskable-512.png","revision":"33eae128615a06b6"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
