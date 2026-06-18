const CACHE_NAME="aura-precache-f846b8ec70e208cf";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"c513708f0a839146"},{"url":"/assets/AppShell-F9rIgiSp.js","revision":"291b8f4795722b4f"},{"url":"/assets/ExternalStoreChat-wv1_GK1s.js","revision":"d237b6f7eb730fda"},{"url":"/assets/index-0hOJgcbN.css","revision":"35ccb80325a1db58"},{"url":"/assets/index-BvWsiIyM.js","revision":"8e86f9dc9f7240b9"},{"url":"/assets/LoginPage-CDoycz2H.js","revision":"4ff083886b3cae58"},{"url":"/assets/NotFoundView-DHqUrx9g.js","revision":"39441dd639a8bd4e"},{"url":"/assets/ThemeSwitcher-BbqKzcL5.js","revision":"535f1a504accd39b"},{"url":"/assets/useConversations-1OxRk3Y4.js","revision":"e882bfab17b23404"},{"url":"/assets/useTranslation-C9hHPQSY.js","revision":"ea6365c0921085da"},{"url":"/favicon.svg","revision":"efe40f6f3218842d"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"5afa3c5decd2b269"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"8cb445cd2e408732"},{"url":"/pwa-512.png","revision":"5afa3c5decd2b269"},{"url":"/pwa-maskable-512.png","revision":"5afa3c5decd2b269"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
