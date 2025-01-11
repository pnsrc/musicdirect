let tracks = [];
let currentTrackIndex = 0;
let player = null;
let progressInterval;
let currentSortKey = 'position'; // Default sort by position

async function loadTrackList() {
  try {
    const response = await fetch('http://localhost:8080/api/tracks');
    if (!response.ok) throw new Error('Ошибка сети');
    tracks = await response.json();

    // Sort tracks based on current sort key
    sortTracks();

    const trackListContainer = document.getElementById('track-list');
    trackListContainer.innerHTML = '';

    // Add sort controls
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

    // Add tracks
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

// Функция для удаления трека
function deleteTrack(trackId) {
  // Подтверждение удаления
  if (!confirm('Вы уверены, что хотите удалить этот трек?')) {
      return;
  }

  // Отправка запроса на удаление
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
      // Если удаление прошло успешно, удаляем элемент из DOM
      const trackElement = document.querySelector(`[data-track-id="${trackId}"]`);
      if (trackElement) {
          trackElement.remove();
      }
      // Показываем уведомление об успешном удалении
      showNotification('Трек успешно удален');
      // Обновляем плейлист
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

  // Change accent color
  const hue = Math.floor(Math.random() * 360);
  document.documentElement.style.setProperty('--accent-color', `hsl(${hue}, 84%, 60%)`);

  updatePlayPauseIcon(true);
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