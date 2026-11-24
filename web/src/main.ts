import {createApp} from 'vue'
import App from './App.vue'
import './style.css'
import {createHubRouter} from './router'
const router=createHubRouter()
const app=createApp(App).use(router)
router.isReady().then(()=>app.mount('#app'))
