const CACHE_NAME="aura-precache-b8330a544651c7ae";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"c513708f0a839146"},{"url":"/assets/AppShell-Djd1kfbw.js","revision":"a92841c0cbfb18b7"},{"url":"/assets/ExternalStoreChat-CECU8GvR.js","revision":"ccab25863aa56d4c"},{"url":"/assets/index-B2TDS_Ds.js","revision":"b4bbfdfa2c436299"},{"url":"/assets/index-ba66b319.css","revision":"3358b7dca65155eb"},{"url":"/assets/LoginPage-Z0fKJPib.js","revision":"b5a0d799cc091c03"},{"url":"/assets/NotFoundView-QZ64fJ-O.js","revision":"71065aa77f2f608e"},{"url":"/assets/ThemeSwitcher-BlFM1usc.js","revision":"d3a4be8c60280a03"},{"url":"/assets/useConversations-K0572Jcb.js","revision":"4fa37c3d1a1dabad"},{"url":"/assets/useTranslation-CsBLzdU8.js","revision":"7608a22403d2dcc2"},{"url":"/favicon.svg","revision":"efe40f6f3218842d"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"5afa3c5decd2b269"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"8cb445cd2e408732"},{"url":"/pwa-512.png","revision":"5afa3c5decd2b269"},{"url":"/pwa-maskable-512.png","revision":"5afa3c5decd2b269"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
