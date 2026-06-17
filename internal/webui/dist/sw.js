const CACHE_NAME="aura-precache-019c653e13c312a7";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"1b368f466faf5113"},{"url":"/assets/AppShell-BN8PnR57.js","revision":"92feb90e1199d997"},{"url":"/assets/index-Die3B4Je.css","revision":"bcb5ae451cb784b4"},{"url":"/assets/index-M7kzbA11.js","revision":"3c0b713db81ada02"},{"url":"/assets/LanguageSwitcher-DVIZYWul.js","revision":"f688bf533f96846d"},{"url":"/assets/LoginPage-tizs2mui.js","revision":"c536b085d7d1ed64"},{"url":"/assets/NotFoundView-SBfZ5wmG.js","revision":"b56df17c9c323bba"},{"url":"/assets/useTranslation-CW9IMJLM.js","revision":"4094445811a3f82a"},{"url":"/favicon.svg","revision":"b4e1473c65c90bf8"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"10a648d242ed05ba"},{"url":"/manifest.webmanifest","revision":"56e207c40260c0da"},{"url":"/pwa-192.png","revision":"8fad95a5f7364b80"},{"url":"/pwa-512.png","revision":"f70794c5678d3cd1"},{"url":"/pwa-maskable-512.png","revision":"f5d9b3da931806c1"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
