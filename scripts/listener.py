from time import sleep
import io
import requests
import base64
import sounddevice as sd
from scipy.io.wavfile import write, read

backend_url = "http://backend:5001"

def rec_audio(fs, seconds):
  myrec = sd.rec(int(seconds * fs), samplerate=fs, channels=1, dtype="int16")
  sd.wait()  # Wait until recording is finished

  wav_buffer = io.BytesIO() # save file to memory as bytes
  write(wav_buffer, fs, myrec)

  audio_b64 = base64.b64encode(wav_buffer.getvalue()).decode("utf-8") # convert to base64 vaw

  payload = {
      "audio": audio_b64,
      "channels": 1,
      "sampleRate": fs,
      "sampleSize": 16,
      "duration": seconds,
      #"latitude": None,
      #"longitude": None
  }

  return payload

def listen():
  fs = 44100  # Sample rate
  seconds = 2  # Duration of recording
  fs, audio = read("danger.wav")

  while True:
    payload = rec_audio(fs, seconds)
    response = requests.post(
        backend_url + "/api/audio/classify",
        json=payload
    )

    if response.json()["isDrone"]:
      sd.play(audio, fs)
      sd.wait()

    
    print(response.status_code, flush = True)
    print(response.text)
    print(response.json()["isDrone"])
    sleep(5)

if __name__ == "__main__":
  listen()