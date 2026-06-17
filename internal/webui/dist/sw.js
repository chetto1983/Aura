const CACHE_NAME="aura-precache-f6e2e7878a001d83";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"1b368f466faf5113"},{"url":"/assets/AppShell-wQpDZvtY.js","revision":"75d67fda83e6b35e"},{"url":"/assets/index-bDTmS-HB.js","revision":"c2ec61b68c6cc747"},{"url":"/assets/index-C2MlGBcY.css","revision":"92d400764f9c1ab0"},{"url":"/assets/LanguageSwitcher-CGqHGeSe.js","revision":"024663ea7dbc07cc"},{"url":"/assets/LoginPage-BXu9JPzk.js","revision":"392cf2437fc127e5"},{"url":"/assets/NotFoundView-CN5BGDuc.js","revision":"ef0cbfa59a77952f"},{"url":"/assets/useTranslation-4WPXxvzj.js","revision":"acea291f25d3b2a0"},{"url":"/favicon.svg","revision":"b4e1473c65c90bf8"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"10a648d242ed05ba"},{"url":"/manifest.webmanifest","revision":"56e207c40260c0da"},{"url":"/pwa-192.png","revision":"8fad95a5f7364b80"},{"url":"/pwa-512.png","revision":"f70794c5678d3cd1"},{"url":"/pwa-maskable-512.png","revision":"f5d9b3da931806c1"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
