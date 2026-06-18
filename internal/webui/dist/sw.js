const CACHE_NAME="aura-precache-42aa6d98a7ec82e6";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"c513708f0a839146"},{"url":"/assets/AppShell-Cm4BJHtq.js","revision":"b04e3edd5738411c"},{"url":"/assets/ExternalStoreChat-CxDBjLQG.js","revision":"9ee38e549dbdc8f3"},{"url":"/assets/index-DOv0AYtY.js","revision":"32cdad864255df60"},{"url":"/assets/index-7ff9c6e0.css","revision":"233f8a583f2728b5"},{"url":"/assets/LoginPage-BYvIsKC3.js","revision":"78af106506a7663e"},{"url":"/assets/NotFoundView-BaUcvUfQ.js","revision":"8f6cecd2d8fb8534"},{"url":"/assets/ThemeSwitcher-B63Ywa4R.js","revision":"e37d4811c93ca24e"},{"url":"/assets/useConversations-DGupawjl.js","revision":"f51560b09aa305e6"},{"url":"/assets/useTranslation-BeQzn_gD.js","revision":"6be187b9fd043628"},{"url":"/favicon.svg","revision":"efe40f6f3218842d"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"5afa3c5decd2b269"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"8cb445cd2e408732"},{"url":"/pwa-512.png","revision":"5afa3c5decd2b269"},{"url":"/pwa-maskable-512.png","revision":"5afa3c5decd2b269"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
