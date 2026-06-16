const CACHE_NAME="aura-precache-19dd24c1c79a4971";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"1b368f466faf5113"},{"url":"/assets/AppShell-HMbT6Bho.js","revision":"282cf6042b505784"},{"url":"/assets/index-Cp4idgjk.js","revision":"f68fb111ba835ea2"},{"url":"/assets/index-DAM1aZ3O.css","revision":"2d69cdfe64db2e10"},{"url":"/assets/LanguageSwitcher-B6qbfw7u.js","revision":"c2b56b448012897e"},{"url":"/assets/LoginPage-fCWK7KHJ.js","revision":"76e052d26d8e9125"},{"url":"/assets/NotFoundView-B1hIkKnJ.js","revision":"7bff6a127a8619d2"},{"url":"/assets/useTranslation-CstvfOLC.js","revision":"58cc1774c51f0e85"},{"url":"/favicon.svg","revision":"b4e1473c65c90bf8"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"10a648d242ed05ba"},{"url":"/manifest.webmanifest","revision":"56e207c40260c0da"},{"url":"/pwa-192.png","revision":"8fad95a5f7364b80"},{"url":"/pwa-512.png","revision":"f70794c5678d3cd1"},{"url":"/pwa-maskable-512.png","revision":"f5d9b3da931806c1"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
