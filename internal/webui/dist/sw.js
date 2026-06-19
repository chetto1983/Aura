const CACHE_NAME="aura-precache-3fb47cfdda535ad2";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"5508e1ffcdd6c2eb"},{"url":"/assets/AppShell-BHRf7lV4.js","revision":"c6b082abfb62f952"},{"url":"/assets/assistant-ui-D7JgFyK7.js","revision":"a123bba29a8c38f8"},{"url":"/assets/authConfig-BVwKa--T.js","revision":"e01f0480819ccfc2"},{"url":"/assets/ExternalStoreChat-B3usTL9e.js","revision":"ca2ead831934d8a3"},{"url":"/assets/focusTrap-DdNrkDrW.js","revision":"0d4425b04929fbbf"},{"url":"/assets/GraphExplorer-Bf5Di43b.js","revision":"263b2f4224f2fca7"},{"url":"/assets/index-CDnc6Cyv.js","revision":"8a7c2ddbbd73ded1"},{"url":"/assets/index-DAB4iUqu.css","revision":"692e8b38ade824bb"},{"url":"/assets/LoginPage-Z195rTMA.js","revision":"d778cd1190a7fd83"},{"url":"/assets/NotFoundView-DWKYsPhU.js","revision":"5f17f114fcfc8aba"},{"url":"/assets/rolldown-runtime-BUR9erT_.js","revision":"66bc2dc82ec39095"},{"url":"/assets/shiki-core-BZGQo0dP.js","revision":"855d00a071c225de"},{"url":"/assets/shiki-lang-js-KbSxL_Iu.js","revision":"e81d53e8da7ecf78"},{"url":"/assets/shiki-lang-tools-BNhm00f6.js","revision":"ba6200b72fd342ee"},{"url":"/assets/shiki-lang-web-CTkMxnFF.js","revision":"68fbe9995ce02ec1"},{"url":"/assets/shiki-themes-_UjgcaiI.js","revision":"6fbd6a859ed63c33"},{"url":"/assets/SigmaCanvas-_80uNFgq.js","revision":"925919778510de6c"},{"url":"/assets/SigmaCanvas-gwv2EvJr.css","revision":"20e692f226ef2093"},{"url":"/assets/sourceExplorerControls-BFvIgvZN.js","revision":"1e0df4a5b3d8630c"},{"url":"/assets/useTranslation-H603BpEd.js","revision":"c20173357c6c2756"},{"url":"/favicon.svg","revision":"7b17af2ead77936a"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"3a814571ad551c73"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"c32d08b35f2cc4e2"},{"url":"/pwa-512.png","revision":"07720c285ef365f3"},{"url":"/pwa-maskable-512.png","revision":"33eae128615a06b6"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
