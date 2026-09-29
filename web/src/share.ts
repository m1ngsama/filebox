import { mount } from 'svelte'
import './boot'
import SharePage from './routes/SharePage.svelte'

mount(SharePage, { target: document.getElementById('app')!, props: { token: decodeURIComponent(location.pathname.split('/')[2] ?? '') } })
