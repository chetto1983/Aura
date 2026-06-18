const CACHE_NAME="aura-precache-dd994b5429c88d76";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"5508e1ffcdd6c2eb"},{"url":"/assets/AppShell-Cm4BJHtq.js","revision":"b04e3edd5738411c"},{"url":"/assets/ExternalStoreChat-CxDBjLQG.js","revision":"9ee38e549dbdc8f3"},{"url":"/assets/index-DOv0AYtY.js","revision":"32cdad864255df60"},{"url":"/assets/index-7ff9c6e0.css","revision":"233f8a583f2728b5"},{"url":"/assets/LoginPage-BYvIsKC3.js","revision":"78af106506a7663e"},{"url":"/assets/NotFoundView-BaUcvUfQ.js","revision":"8f6cecd2d8fb8534"},{"url":"/assets/ThemeSwitcher-B63Ywa4R.js","revision":"e37d4811c93ca24e"},{"url":"/assets/useConversations-DGupawjl.js","revision":"f51560b09aa305e6"},{"url":"/assets/useTranslation-BeQzn_gD.js","revision":"6be187b9fd043628"},{"url":"/favicon.svg","revision":"7b17af2ead77936a"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"3a814571ad551c73"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"c32d08b35f2cc4e2"},{"url":"/pwa-512.png","revision":"07720c285ef365f3"},{"url":"/pwa-maskable-512.png","revision":"33eae128615a06b6"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
