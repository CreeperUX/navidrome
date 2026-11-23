// The player in Sonus's colours (as dark.css.js does for Dark): amber where Dark has its blue
const stylesheet = `

.react-jinke-music-player-main svg:active, .react-jinke-music-player-main svg:hover {
    color: #ffc522
}

.react-jinke-music-player-main .music-player-panel {
    background-color: #0b0f11;
    border-top: 1px solid rgba(239, 235, 229, 0.08)
}

.react-jinke-music-player-main .music-player-panel .panel-content .rc-slider-handle, .react-jinke-music-player-main .music-player-panel .panel-content .rc-slider-track {
    background-color: #f8b700
}

.react-jinke-music-player-main ::-webkit-scrollbar-thumb {
    background-color: #f8b700;
}

.react-jinke-music-player-main .music-player-panel .panel-content .rc-slider-handle:active {
    box-shadow: 0 0 2px #f8b700
}

.react-jinke-music-player-main .audio-item.playing svg {
    color: #f8b700
}

.react-jinke-music-player-main .audio-item.playing .player-singer {
    color: #f8b700 !important
}

.audio-lists-panel-content .audio-item.playing, .audio-lists-panel-content .audio-item.playing svg {
    color: #f8b700
}
.audio-lists-panel-content .audio-item:active .group:not([class=".player-delete"]) svg, .audio-lists-panel-content .audio-item:hover .group:not([class=".player-delete"]) svg {
    color: #f8b700
}
`

export default stylesheet
