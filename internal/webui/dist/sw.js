const CACHE_NAME="aura-precache-e72ec88cb37d7b92";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"1b368f466faf5113"},{"url":"/assets/AppShell-BjYFLKg6.js","revision":"b0c87264ca54fe44"},{"url":"/assets/index-C30Tbeop.js","revision":"1981bee835c3650c"},{"url":"/assets/index-DAiD7ulm.css","revision":"cabeeea0654f21e4"},{"url":"/assets/LanguageSwitcher-BZWvrfeq.js","revision":"0da2b89d85cb9f62"},{"url":"/assets/LoginPage-BTOfEVaC.js","revision":"f9b250804acca927"},{"url":"/assets/NotFoundView-Cm45QZDH.js","revision":"0b1cc7ac61e8c8ca"},{"url":"/assets/useTranslation-BI9Llna7.js","revision":"0ee78a84d57df633"},{"url":"/favicon.svg","revision":"b4e1473c65c90bf8"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"10a648d242ed05ba"},{"url":"/manifest.webmanifest","revision":"56e207c40260c0da"},{"url":"/pwa-192.png","revision":"8fad95a5f7364b80"},{"url":"/pwa-512.png","revision":"f70794c5678d3cd1"},{"url":"/pwa-maskable-512.png","revision":"f5d9b3da931806c1"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
