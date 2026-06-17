const CACHE_NAME="aura-precache-2ca6dc3caf1db6d9";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"1b368f466faf5113"},{"url":"/assets/AppShell-CplI_9lT.js","revision":"1719af97441d3d59"},{"url":"/assets/index-DBoLd6IS.js","revision":"9d10e3dd774cfd38"},{"url":"/assets/index-Die3B4Je.css","revision":"bcb5ae451cb784b4"},{"url":"/assets/LanguageSwitcher-ehPWwvBY.js","revision":"15f51eb04b6393c7"},{"url":"/assets/LoginPage-CZwDkK_W.js","revision":"92664133bfa07568"},{"url":"/assets/NotFoundView-Dlny-Ktq.js","revision":"7ad29e1b8d81141c"},{"url":"/assets/useTranslation-DaAbwhie.js","revision":"e611b7d4e4a4f793"},{"url":"/favicon.svg","revision":"b4e1473c65c90bf8"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"10a648d242ed05ba"},{"url":"/manifest.webmanifest","revision":"56e207c40260c0da"},{"url":"/pwa-192.png","revision":"8fad95a5f7364b80"},{"url":"/pwa-512.png","revision":"f70794c5678d3cd1"},{"url":"/pwa-maskable-512.png","revision":"f5d9b3da931806c1"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
