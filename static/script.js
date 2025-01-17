let tracks = [];
let currentTrackIndex = 0;
let player = null;
let progressInterval;
let currentSortKey = 'position';
let previousTrackIds = [];

async function checkForPlaylistUpdates() {
  try {
    // /api/tracks/all?room_code=
    const response = await fetch(`/api/tracks/all?room_code=${getRoomCode()}`);
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

// Вспомогательная функция для сравнения массивов
function arraysAreEqual(arr1, arr2) {
  if (arr1.length !== arr2.length) return false;
  return arr1.every((value, index) => value === arr2[index]);
}

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


async function loadTrackList() {
  try {
    const response = await fetch('/api/tracks?room_code=' + getRoomCode());
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

    document.getElementById("room-code").textContent = getRoomCode();

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

// Function to delete a track with proper error handling and room code management
function deleteTrack(trackId) {
  // Get room code from URL query parameter or localStorage
  const urlParams = new URLSearchParams(window.location.search);
  const roomCode = urlParams.get('room') || localStorage.getItem('roomCode');
  
  if (!roomCode) {
      showNotification('Room code not found', 'error');
      return;
  }

  // Confirmation dialog
  if (!confirm('Вы уверены, что хотите удалить этот трек?')) {
      return;
  }

  // Show loading state
  const trackElement = document.querySelector(`[data-track-id="${trackId}"]`);
  if (trackElement) {
      trackElement.style.opacity = '0.5';
  }

  // Send delete request
  fetch('/api/tracks/delete', {
      method: 'POST',
      headers: {
          'Content-Type': 'application/json',
          'Accept': 'application/json'
      },
      body: JSON.stringify({
          track_id: parseInt(trackId), // Ensure trackId is a number
          room_code: roomCode
      })
  })
  .then(response => {
      if (!response.ok) {
          return response.json().then(data => {
              throw new Error(data.message || `HTTP error! status: ${response.status}`);
          });
      }
      return response.json();
  })
  .then(data => {
      if (data.success) {
          // Remove track element from DOM
          if (trackElement) {
              trackElement.remove();
          }
          
          // Update playlist counter if it exists
          updatePlaylistCounter();
          
          // Show success notification
          showNotification('Трек успешно удален', 'success');
          
          // Broadcast update to other clients via WebSocket if available
          if (window.socket && window.socket.readyState === WebSocket.OPEN) {
              window.socket.send(JSON.stringify({
                  type: 'trackDeleted',
                  trackId: trackId,
                  roomCode: roomCode
              }));
          }
      } else {
          throw new Error(data.message || 'Failed to delete track');
      }
  })
  .catch(error => {
      console.error('Error deleting track:', error);
      // Restore track element opacity
      if (trackElement) {
          trackElement.style.opacity = '1';
      }
      showNotification(`Ошибка при удалении трека: ${error.message}`, 'error');
  });
}

// Helper function to update playlist counter
function updatePlaylistCounter() {
  const counter = document.querySelector('.playlist-counter');
  if (counter) {
      const currentCount = parseInt(counter.textContent) - 1;
      counter.textContent = currentCount;
  }
}

// Helper function to show notifications
function showNotification(message, type = 'success') {
  const notification = document.createElement('div');
  notification.className = `notification ${type}`;
  notification.textContent = message;
  
  // Style the notification
  Object.assign(notification.style, {
      position: 'fixed',
      top: '20px',
      right: '20px',
      padding: '10px 20px',
      borderRadius: '4px',
      backgroundColor: type === 'success' ? '#4CAF50' : '#f44336',
      color: 'white',
      zIndex: '1000',
      opacity: '0',
      transition: 'opacity 0.3s ease'
  });
  
  document.body.appendChild(notification);
  
  // Fade in
  setTimeout(() => {
      notification.style.opacity = '1';
  }, 10);
  
  // Remove after 3 seconds
  setTimeout(() => {
      notification.style.opacity = '0';
      setTimeout(() => {
          notification.remove();
      }, 300);
  }, 3000);
}

// Функция обновления плейлиста
function updatePlaylist() {
  fetch('/api/tracks?room_code=' + getRoomCode())
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
      const response = await fetch('/add-track?=room_code=' + getRoomCode(), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ track_url: trackUrl, room_code: getRoomCode() }),
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
// Проверка обновлений плейлиста каждые 5 секунд
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

// обрабатываем {"type":"next"} {"type":"pause"} {"type":"now"} {"type":"prev"} и тп от ws switch case

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
      // отпраляем текущий трек на сервер
      showNotification('Короче както лень');
      break;
    case 'prev':
      currentTrackIndex = (currentTrackIndex - 1 + tracks.length) % tracks.length;
      playTrack(currentTrackIndex);
      break;
  }
}

// если ws не доступен то показываем уведомление Ебать, сервак наебнулся поднимай
socket.onclose = () => {
  showNotification('Ебать, сервак наебнулся поднимай');
}

function getRoomCode() {
  return localStorage.getItem('room_code');
}

function setRoomCode(roomCode) {
  localStorage.setItem('room_code', roomCode);
}

function clearRoomCode() {
  localStorage.removeItem('room_code');
}

const roomCode = getRoomCode();
const oopsElement = document.querySelector('.oops');

if (roomCode && oopsElement) {
  oopsElement.style.display = 'none';
}

function createRoom() {
  fetch('/api/room/create')
    .then((response) => {
      if (!response.ok) {
        throw new Error('Ошибка сети при создании комнаты');
      }
      return response.json();
    })
    .then((data) => {
      if (data.code) {
        setRoomCode(data.code);
        if (oopsElement) {
          oopsElement.style.display = 'none';
        }
      } else {
        throw new Error('Не удалось получить код комнаты');
      }
    })
    .catch((error) => {
      console.error('Error:', error);
      showNotification('Ошибка при создании комнаты: ' + error.message, 'error');
    });
}

// подключение успешно если пришло это {"room_id":1}
function joinRoom(roomCode) {
  fetch('/api/room/join', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ room_code: roomCode }),
  })
    .then((response) => {
      if (!response.ok) {
        throw new Error('Ошибка сети при подключении к комнате');
      }
      return response.json();
    })
    .then((data) => {
      if (data.room_id) {
        setRoomCode(roomCode);
        if (oopsElement) {
          oopsElement.style.display = 'none';
        }
      } else {
        throw new Error('Не удалось подключиться к комнате');
      }
    })
    .catch((error) => {
      console.error('Error:', error);
      showNotification('Ошибка при подключении к комнате: ' + error.message, 'error');
    });
}


document.querySelector('.create')?.addEventListener('click', createRoom);

document.querySelector('.join')?.addEventListener('click', () => {
  const roomCodeInput = document.querySelector('.input');
  if (roomCodeInput) {
    const roomCode = roomCodeInput.value;
    joinRoom(roomCode);
  }
});

function toggleShuffle() {
  const shuffleButton = document.getElementById('shuffle');
  const isShuffled = shuffleButton.classList.contains('active');
  if (isShuffled) {
    shuffleButton.classList.remove('active');
    tracks = tracks.sort((a, b) => a.position - b.position);
  } else {
    shuffleButton.classList.add('active');
    tracks = shuffle(tracks);
  }
  loadTrackList();
}

function shuffle(array) {
  const shuffled = [...array];
  for (let i = shuffled.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    [shuffled[i], shuffled[j]] = [shuffled[j], shuffled[i]];
  }
  return shuffled;
}
