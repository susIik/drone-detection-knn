FROM python:3.12-slim

WORKDIR /app

RUN mkdir -p /root/panns_data && \
    python -c "import urllib.request; urllib.request.urlretrieve('https://zenodo.org/record/3987831/files/Cnn14_mAP%3D0.431.pth?download=1','/root/panns_data/Cnn14_mAP=0.431.pth'); urllib.request.urlretrieve('http://storage.googleapis.com/us_audioset/youtube_corpus/v1/csv/class_labels_indices.csv','/root/panns_data/class_labels_indices.csv')"

COPY embedding_service.py requirements-panns.txt ./
RUN pip install --no-cache-dir -r requirements-panns.txt

CMD ["python", "./embedding_service.py"]