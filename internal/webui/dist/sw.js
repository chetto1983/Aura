const CACHE_NAME="aura-precache-637c49104bfb1a19";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"c513708f0a839146"},{"url":"/assets/AppShell-B6lWsoOS.js","revision":"444f7198bb952773"},{"url":"/assets/ExternalStoreChat-DE0e1Khv.js","revision":"c94c325c0e45f1a1"},{"url":"/assets/index-0hOJgcbN.css","revision":"35ccb80325a1db58"},{"url":"/assets/index-Deaq_1Px.js","revision":"d0b579ee368ced7f"},{"url":"/assets/LoginPage--TAGf-j4.js","revision":"9943a0ae6b7f1ae8"},{"url":"/assets/NotFoundView-BWMLcZiK.js","revision":"a5a6bb103a05bfb6"},{"url":"/assets/ThemeSwitcher-YVaGGXEd.js","revision":"ee02572f3986e588"},{"url":"/assets/useConversations-DBXFdeFb.js","revision":"7867385113c9eb15"},{"url":"/assets/useTranslation-B2BINUag.js","revision":"5817b171d6bdf9b7"},{"url":"/favicon.svg","revision":"efe40f6f3218842d"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"5afa3c5decd2b269"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"8cb445cd2e408732"},{"url":"/pwa-512.png","revision":"5afa3c5decd2b269"},{"url":"/pwa-maskable-512.png","revision":"5afa3c5decd2b269"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
