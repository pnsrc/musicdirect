let tracks = [];
let currentTrackIndex = 0;
let player = null;
let progressInterval;
let currentSortKey = 'position'; // Default sort by position
let localPeer, remotePeer, localStream, remoteStream;
let socket = new WebSocket('ws://localhost:8080/ws'); // Ваш WebSocket сервер

// WebRTC: Получение аудио
navigator.mediaDevices.getUserMedia({ audio: true })
  .then((stream) => {
    localStream = stream;
    document.getElementById('remoteAudio').srcObject = stream; // Для локального теста
  })
  .catch((err) => {
    console.error("Error accessing audio: ", err);
  });

async function loadTrackList() {
  try {
    const response = await fetch('http://localhost:8080/api/tracks');
    if (!response.ok) throw new Error('Ошибка сети');
    tracks = await response.json();

    // Сортировка треков
    sortTracks();

    const trackListContainer = document.getElementById('track-list');
    trackListContainer.innerHTML = '';

    // Добавление элементов управления сортировкой
    const sortControls = document.createElement('div');
    sortControls.className = 'sort-controls mb-3';
    sortControls.innerHTML = `
      <div class="btn-group">
        <button class="btn btn-sm ${currentSortKey === 'position' ? 'btn-primary' : 'btn-outline-primary'}" 
                onclick="changeSortKey('position')">
          По позиции
        </button>
        <button class="btn btn-sm ${currentSortKey === 'title' ? 'btn-primary' : 'btn-outline-primary'}" 
                onclick="changeSortKey('title')">
          По названию
        </button>
      </div>
    `;
    trackListContainer.appendChild(sortControls);

    // Добавление треков в список
    tracks.forEach((track, index) => {
      const trackItem = document.createElement('div');
      trackItem.className = 'track';
      trackItem.dataset.index = index;
      trackItem.innerHTML = `
        <img src="https://${track.cover_uri}400x400" alt="${track.title}">
        <div class="track-info">
          <div class="track-title">${track.title}</div>
          <div class="track-artist">${track.artist}</div>
        </div>
      `;
      trackItem.addEventListener('click', () => playTrack(index));
      trackListContainer.appendChild(trackItem);
    });
  } catch (error) {
    console.error('Ошибка загрузки треков:', error);
  }
}

function sortTracks() {
  tracks.sort((a, b) => {
    if (currentSortKey === 'position') {
      return (a.position || 0) - (b.position || 0);
    } else if (currentSortKey === 'title') {
      return a.title.localeCompare(b.title);
    }
    return 0;
  });
}

function changeSortKey(newSortKey) {
  currentSortKey = newSortKey;
  loadTrackList();
}

function playTrack(index) {
  if (player) player.stop();
  player = new Howl({
    src: [tracks[index].track_url],
    html5: true,
    onend: () => playNext(),
    onplay: updateProgress
  });
  player.play();
  updateProgress();
  updateMediaSession(tracks[index]);
  currentTrackIndex = index;

  const track = tracks[index];
  document.getElementById('current-track-title').textContent = track.title;
  document.getElementById('current-track-artist').textContent = track.artist;
  document.getElementById('cover-img').src = `https://${track.cover_uri}600x600`;

  // Изменение акцентного цвета
  const hue = Math.floor(Math.random() * 360);
  document.documentElement.style.setProperty('--accent-color', `hsl(${hue}, 84%, 60%)`);

  updatePlayPauseIcon(true);

  if (localStream) {
    if (!localPeer) {
      localPeer = new SimplePeer({
        initiator: true,
        trickle: false,
        stream: localStream
      });

      localPeer.on('signal', (data) => {
        console.log('Sending offer: ', data);
        socket.send(JSON.stringify({ offer: data }));
      });

      localPeer.on('stream', (stream) => {
        console.log('Received remote stream');
        remoteStream = stream;
        document.getElementById('remoteAudio').srcObject = remoteStream;
      });

      localPeer.on('iceCandidate', (candidate) => {
        socket.send(JSON.stringify({ iceCandidate: candidate }));
      });
    }
  }
}

function playNext() {
  currentTrackIndex = (currentTrackIndex + 1) % tracks.length;
  playTrack(currentTrackIndex);
}

function updateProgress() {
  if (progressInterval) clearInterval(progressInterval);
  progressInterval = setInterval(() => {
    const currentTime = player.seek() || 0;
    const duration = player.duration() || 0;
    const progress = (currentTime / duration) * 100;

    document.getElementById('progress').style.width = `${progress}%`;
    document.getElementById('current-time').textContent = `${formatTime(currentTime)} / ${formatTime(duration)}`;
  }, 1000);
}

function updatePlayPauseIcon(isPlaying) {
  const icon = isPlaying ? '<i class="fas fa-pause"></i>' : '<i class="fas fa-play"></i>';
  document.getElementById('play-pause').innerHTML = icon;
}

function formatTime(seconds) {
  const mins = Math.floor(seconds / 60);
  const secs = Math.floor(seconds % 60);
  return `${mins}:${secs < 10 ? '0' : ''}${secs}`;
}

socket.onopen = () => {
  console.log('WebSocket connected');
};

socket.onmessage = (event) => {
  const message = JSON.parse(event.data);

  if (message.offer) {
    handleOffer(message.offer);
  } else if (message.answer) {
    handleAnswer(message.answer);
  } else if (message.iceCandidate) {
    handleIceCandidate(message.iceCandidate);
  }
};

function handleOffer(offer) {
  if (!remotePeer) {
    remotePeer = new SimplePeer({
      initiator: false,
      trickle: false,
      stream: localStream
    });

    remotePeer.on('signal', (data) => {
      console.log('Sending answer: ', data);
      socket.send(JSON.stringify({ answer: data }));
    });

    remotePeer.on('stream', (stream) => {
      console.log('Received remote stream');
      remoteStream = stream;
      document.getElementById('remoteAudio').srcObject = remoteStream;
    });

    remotePeer.on('iceCandidate', (candidate) => {
      socket.send(JSON.stringify({ iceCandidate: candidate }));
    });
  }
  remotePeer.signal(offer);
}

function handleAnswer(answer) {
  if (localPeer) {
    localPeer.signal(answer);
  }
}

function handleIceCandidate(candidate) {
  if (localPeer) {
    localPeer.addIceCandidate(candidate);
  }
  if (remotePeer) {
    remotePeer.addIceCandidate(candidate);
  }
}

function updateMediaSession(track) {
  navigator.mediaSession.metadata = new MediaMetadata({
    title: track.title,
    artist: track.artist,
    album: track.album,
    artwork: [{ src: `https://${track.cover_uri}200x200`, sizes: '200x200', type: 'image/jpeg' }]
  });
}

document.getElementById('add-track-btn').addEventListener('click', async () => {
  const trackUrl = document.getElementById('track-url').value;
  if (trackUrl) {
    try {
      const response = await fetch('http://localhost:8080/add-track', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ track_url: trackUrl }),
      });
      if (response.ok) {
        loadTrackList();
        const modalElement = document.getElementById('addTrackModal');
        const modal = bootstrap.Modal.getInstance(modalElement);
        modal.hide();
      } else {
        console.error('Ошибка добавления трека');
      }
    } catch (error) {
      console.error('Ошибка добавления трека:', error);
    }
  }
});

loadTrackList();
