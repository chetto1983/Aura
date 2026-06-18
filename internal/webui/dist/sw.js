const CACHE_NAME="aura-precache-845afa2724a4fbbb";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"c513708f0a839146"},{"url":"/assets/AppShell-gAltWR00.js","revision":"1d0ee00a2088b11c"},{"url":"/assets/ExternalStoreChat-CqRS-kyw.js","revision":"fe2fae4613b5eb25"},{"url":"/assets/index-B0K4e-eQ.js","revision":"0bc5d3fcb479ce31"},{"url":"/assets/index-ba66b319.css","revision":"3358b7dca65155eb"},{"url":"/assets/LoginPage-CgMMvosa.js","revision":"64392906b705e71d"},{"url":"/assets/NotFoundView-DO1ka6nw.js","revision":"9340282183427d74"},{"url":"/assets/ThemeSwitcher-DMivHLQq.js","revision":"48b4a25ae2719c1b"},{"url":"/assets/useConversations-DIzbdQSi.js","revision":"0b9333ed8ddc64b0"},{"url":"/assets/useTranslation-C7yjgQRg.js","revision":"e5fede817a530056"},{"url":"/favicon.svg","revision":"efe40f6f3218842d"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"5afa3c5decd2b269"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"8cb445cd2e408732"},{"url":"/pwa-512.png","revision":"5afa3c5decd2b269"},{"url":"/pwa-maskable-512.png","revision":"5afa3c5decd2b269"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
