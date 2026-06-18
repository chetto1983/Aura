const CACHE_NAME="aura-precache-07e98c946d2b120a";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"c513708f0a839146"},{"url":"/assets/AppShell-sWvLCTOr.js","revision":"97f56dff41981802"},{"url":"/assets/ExternalStoreChat-C7qSWxhn.js","revision":"bce3839b147574a7"},{"url":"/assets/index-622acfef.css","revision":"35ccb80325a1db58"},{"url":"/assets/index-ZeVuqVUL.js","revision":"d3dd8a2904cdda9d"},{"url":"/assets/LoginPage-BxKLPHJX.js","revision":"0f0928be1ab22575"},{"url":"/assets/NotFoundView-B7CZbuvg.js","revision":"22010371c2f3af2c"},{"url":"/assets/ThemeSwitcher-Dv-fszU8.js","revision":"35e85cf397491242"},{"url":"/assets/useConversations-C_fi7oge.js","revision":"3f1a11c3b7d5ba3f"},{"url":"/assets/useTranslation-D6fGGeyr.js","revision":"1f0969c3433c46a8"},{"url":"/favicon.svg","revision":"efe40f6f3218842d"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"5afa3c5decd2b269"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"8cb445cd2e408732"},{"url":"/pwa-512.png","revision":"5afa3c5decd2b269"},{"url":"/pwa-maskable-512.png","revision":"5afa3c5decd2b269"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
