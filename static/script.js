let tracks = [];
let currentTrackIndex = 0;
let player = null;
let progressInterval;
let currentSortKey = 'position';
let previousTrackIds = [];
let isShuffleEnabled = false;
let shuffledIndexes = [];
let volume = 1.0;
let prevVolume = 1.0;

async function checkForPlaylistUpdates() {
  try {
    const response = await fetch('/api/tracks/all');
    if (!response.ok) throw new Error('Ошибка сети');

    const currentTrackIds = await response.json();

    // Сравниваем с предыдущими треками
    if (!arraysAreEqual(currentTrackIds, previousTrackIds)) {
      console.log('Обнаружены изменения в плейлисте, обновляем...');
      previousTrackIds = [...currentTrackIds]; // Создаём копию массива
      await loadTrackList(); // Обновляем отображение плейлиста
    }
  } catch (error) {
    console.error('Ошибка проверки обновлений плейлиста:', error);
  }
}

function arraysAreEqual(arr1, arr2) {
  if (arr1.length !== arr2.length) return false;
  return arr1.every((value, index) => value === arr2[index]);
}

function shuffleArray(array) {
  const shuffled = [...array];
  for (let i = shuffled.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    [shuffled[i], shuffled[j]] = [shuffled[j], shuffled[i]];
  }
  return shuffled;
}

function toggleShuffle() {
  isShuffleEnabled = !isShuffleEnabled;
  const shuffleBtn = document.getElementById('shuffle-btn');
  
  if (isShuffleEnabled) {
    shuffleBtn.classList.add('active');
    shuffledIndexes = shuffleArray([...tracks.keys()]);
    const currentIndex = shuffledIndexes.indexOf(currentTrackIndex);
    if (currentIndex !== -1) {
      shuffledIndexes.splice(currentIndex, 1);
      shuffledIndexes.unshift(currentTrackIndex);
    }
    showNotification('Случайное воспроизведение включено');
  } else {
    shuffleBtn.classList.remove('active');
    showNotification('Случайное воспроизведение выключено');
  }
}


async function loadTrackList() {
  try {
    const response = await fetch('/api/tracks');
    if (!response.ok) throw new Error('Ошибка сети');
    tracks = await response.json();

    sortTracks();

    const trackListContainer = document.getElementById('track-list');
    trackListContainer.innerHTML = '';

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
        <div class="track-controls">
          <button class="btn btn-sm btn-danger" onclick="deleteTrack(${track.track_id})">
            <i class="fas fa-trash"></i>
          </button>
         </div>

      `;
      trackItem.addEventListener('click', () => playTrack(index));
      trackListContainer.appendChild(trackItem);
      showNotification('Плейлист обновлен');
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

function deleteTrack(trackId) {
  if (!confirm('Вы уверены, что хотите удалить этот трек?')) {
      return;
  }

  fetch('/api/tracks/delete', {
      method: 'POST',
      headers: {
          'Content-Type': 'application/json'
      },
      body: JSON.stringify({
          track_id: trackId
      })
  })
  .then(response => {
      if (!response.ok) {
          throw new Error('Network response was not ok');
      }
      return response.json();
  })
  .then(data => {
      const trackElement = document.querySelector(`[data-track-id="${trackId}"]`);
      if (trackElement) {
          trackElement.remove();
      }
      showNotification('Трек успешно удален');
      updatePlaylist();
  })
  .catch(error => {
      console.error('Error:', error);
      showNotification('Ошибка при удалении трека', 'error');
  });
}

// Вспомогательная функция для показа уведомлений
function showNotification(message, type = 'success') {
  const notification = document.createElement('div');
  notification.className = `notification ${type}`;
  notification.textContent = message;

  document.body.appendChild(notification);
  
  // Удаляем уведомление через 3 секунды
  setTimeout(() => {
      notification.remove();
  }, 3000);
}

// Функция обновления плейлиста
function updatePlaylist() {
  fetch('/api/tracks')
      .then(response => response.json())
      .then(tracks => {
          const playlist = document.querySelector('.playlist');
          if (playlist) {
              renderTracks(tracks);
          }
      })
      .catch(error => {
          console.error('Error updating playlist:', error);
      });
}

// CSS для уведомлений
const style = document.createElement('style');
style.textContent = `
  .notification {
      position: fixed;
      top: 20px;
      right: 20px;
      padding: 15px 25px;
      border-radius: 4px;
      color: white;
      font-weight: bold;
      z-index: 1000;
      animation: fadeIn 0.3s, fadeOut 0.3s 2.7s;
  }
  
  .notification.success {
      background-color: #4CAF50;
  }
  
  .notification.error {
      background-color: #f44336;
  }
  
  @keyframes fadeIn {
      from { opacity: 0; transform: translateY(-20px); }
      to { opacity: 1; transform: translateY(0); }
  }
  
  @keyframes fadeOut {
      from { opacity: 1; transform: translateY(0); }
      to { opacity: 0; transform: translateY(-20px); }
  }
`;
document.head.appendChild(style);

async function getTrackUrl(trackId) {
  try {
    const response = await fetch(`/api/track?trackId=${trackId}`);
    if (!response.ok) throw new Error('Failed to fetch track URL');
    const data = await response.json();
    return data.track_url;
  } catch (error) {
    console.error('Error fetching track URL:', error);
    throw error;
  }
}


async function playTrack(index) {
  try {
    if (player) player.stop();
    
    const track = tracks[index];
    let trackUrl = track.track_url;
    
    // Try to create player with original URL first
    player = new Howl({
      src: [trackUrl],
      html5: true,
      volume: volume,
      onend: () => playNext(),
      onplay: updateProgress,
      onloaderror: async () => {
        console.log('Failed to load original track URL, trying API endpoint...');
        try {
          // If original URL fails, try to get URL from API
          trackUrl = await getTrackUrl(track.track_id);
          
          // Create new player with fallback URL
          player = new Howl({
            src: [trackUrl],
            html5: true,
            volume: volume,
            onend: () => playNext(),
            onplay: updateProgress,
            onloaderror: () => {
              console.error('Failed to load track even with fallback URL');
              showNotification('Ошибка загрузки трека', 'error');
              playNext(); // Skip to next track on failure
            }
          });
          
          player.play();
        } catch (error) {
          console.error('Failed to get fallback URL:', error);
          showNotification('Ошибка загрузки трека', 'error');
          playNext(); // Skip to next track on failure
        }
      }
    });
    
    player.play();
    updateProgress();
    updateMediaSession(track);
    currentTrackIndex = index;

    document.getElementById('current-track-title').textContent = track.title;
    document.getElementById('current-track-artist').textContent = track.artist;
    document.getElementById('cover-img').src = `https://${track.cover_uri}600x600`;

    const hue = Math.floor(Math.random() * 360);
    document.documentElement.style.setProperty('--accent-color', `hsl(${hue}, 84%, 60%)`);

    updatePlayPauseIcon(true);
    
    document.querySelectorAll('.track').forEach(track => track.classList.remove('active'));
    document.querySelector(`.track[data-index="${index}"]`)?.classList.add('active');
    
  } catch (error) {
    console.error('Error playing track:', error);
    showNotification('Ошибка воспроизведения трека', 'error');
    playNext(); // Skip to next track on failure
  }
}

function playNext() {
  if (isShuffleEnabled) {
    const currentShuffleIndex = shuffledIndexes.indexOf(currentTrackIndex);
    const nextIndex = (currentShuffleIndex + 1) % tracks.length;
    currentTrackIndex = shuffledIndexes[nextIndex];
  } else {
    currentTrackIndex = (currentTrackIndex + 1) % tracks.length;
  }
  playTrack(currentTrackIndex);
}

// Функции управления громкостью
function updateVolume(value) {
  volume = parseFloat(value);
  if (player) {
    player.volume(volume);
  }
  updateVolumeIcon();
  
  // Анимация слайдера
  const volumeSlider = document.getElementById('volume-slider');
  const percentage = volume * 100;
  volumeSlider.style.background = `linear-gradient(to right, var(--accent-color) ${percentage}%, rgba(255, 255, 255, 0.2) ${percentage}%)`;
}

function updateVolumeIcon() {
  const volumeIcon = document.getElementById('volume-icon');
  if (volume === 0) {
    volumeIcon.className = 'fas fa-volume-mute';
  } else if (volume < 0.5) {
    volumeIcon.className = 'fas fa-volume-down';
  } else {
    volumeIcon.className = 'fas fa-volume-up';
  }
}

function toggleMute() {
  const volumeSlider = document.getElementById('volume-slider');
  if (volume > 0) {
    prevVolume = volume;
    updateVolume(0);
    volumeSlider.value = 0;
  } else {
    updateVolume(prevVolume);
    volumeSlider.value = prevVolume;
  }
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

document.getElementById('play-pause').addEventListener('click', () => {
  if (player && player.playing()) {
    player.pause();
    updatePlayPauseIcon(false);
  } else if (player) {
    player.play();
    updatePlayPauseIcon(true);
  }
});

document.getElementById('next').addEventListener('click', playNext);
document.getElementById('prev').addEventListener('click', () => {
  currentTrackIndex = (currentTrackIndex - 1 + tracks.length) % tracks.length;
  playTrack(currentTrackIndex);
});

document.getElementById('progress-bar').addEventListener('click', (event) => {
  const bar = event.currentTarget;
  const rect = bar.getBoundingClientRect();
  const offsetX = event.clientX - rect.left;
  const width = rect.width;
  const percent = offsetX / width;
  const duration = player.duration();
  player.seek(duration * percent);
});

document.addEventListener('keydown', (event) => {
  if (event.code === 'Space') {
    if (player && player.playing()) {
      player.pause();
      updatePlayPauseIcon(false);
    } else if (player) {
      player.play();
      updatePlayPauseIcon(true);
    }
  }
});

navigator.mediaSession.setActionHandler('play', () => {
  if (player) {
    player.play();
    updatePlayPauseIcon(true);
  }
});

navigator.mediaSession.setActionHandler('pause', () => {
  if (player) {
    player.pause();
    updatePlayPauseIcon(false);
  }
});

navigator.mediaSession.setActionHandler('nexttrack', playNext);

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
      const response = await fetch('/add-track', {
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
setInterval(checkForPlaylistUpdates, 5000);

const socket = new WebSocket(`ws://${window.location.host}/ws`);

socket.onmessage = (event) => {
  const message = JSON.parse(event.data);
  if (message.type === 'notification') {
    showNotification(message.message);
  }
};

function showNotification(message) {
  const notification = document.createElement('div');
  notification.className = 'notification success';
  notification.textContent = message;
  document.body.appendChild(notification);

  setTimeout(() => {
    notification.remove();
  }, 3000);
}

showNotification('Подключение к ВебСокету...');

socket.onopen = () => {
  showNotification('Соединение установлено');
}
socket.onmessage = (event) => {
  const message = JSON.parse(event.data);
  switch (message.type) {
    case 'next':
      playNext();
      break;
    case 'pause':
    // она как и проиграть так и пауза
      if (player && player.playing()) {
        player.pause();
        updatePlayPauseIcon(false);
      } else if (player) {
        player.play();
        updatePlayPauseIcon(true);
      }
      break;
    case 'now':
      showNotification('Короче както лень');
      break;
    case 'prev':
      currentTrackIndex = (currentTrackIndex - 1 + tracks.length) % tracks.length;
      playTrack(currentTrackIndex);
      break;
      case 'shuffle':
        toggleShuffle();
        break;
    case 'volume':
      if (message.value !== undefined) {
        updateVolume(message.value);
        document.getElementById('volume-slider').value = message.value;
      }
      break;
  }
}

socket.onclose = () => {
  showNotification('Ебать, сервак наебнулся поднимай');
}

showNotification('Попробуйте телеграм бота @modushuedosbot');