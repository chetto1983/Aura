const CACHE_NAME="aura-precache-e12716b3dfcb9094";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"1b368f466faf5113"},{"url":"/assets/AppShell-Dy44kxut.js","revision":"0773be964e91b1e7"},{"url":"/assets/index-CCpl69i_.js","revision":"dede944730c2e5bd"},{"url":"/assets/index-Dit07tkE.css","revision":"00d957f2d5864cc9"},{"url":"/assets/LanguageSwitcher-C4tfvy4i.js","revision":"8e49ccb470338805"},{"url":"/assets/LoginPage-C2-RpsV8.js","revision":"bae9027e4a759809"},{"url":"/assets/NotFoundView-DqdkPy7b.js","revision":"23fdca56be6fdcfe"},{"url":"/assets/useTranslation-BMtO5nsJ.js","revision":"2ba9b856ace80fee"},{"url":"/favicon.svg","revision":"b4e1473c65c90bf8"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"10a648d242ed05ba"},{"url":"/manifest.webmanifest","revision":"56e207c40260c0da"},{"url":"/pwa-192.png","revision":"8fad95a5f7364b80"},{"url":"/pwa-512.png","revision":"f70794c5678d3cd1"},{"url":"/pwa-maskable-512.png","revision":"f5d9b3da931806c1"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
